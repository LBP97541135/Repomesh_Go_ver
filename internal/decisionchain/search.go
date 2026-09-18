package decisionchain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// SimilarQuery is one recall request. Mode: auto (default), semantic or
// structural; auto prefers semantic and transparently degrades (D11).
type SimilarQuery struct {
	Requirement     string
	RepositoryNames []string
	TopK            int
	MinSimilarity   float64
	Mode            string
}

// SimilarHit is one recalled decision sheet with its score and, for
// structural hits, the repositories that intersected (explainability).
type SimilarHit struct {
	DecisionID           string         `json:"decisionId"`
	RequirementText      string         `json:"requirementText"`
	Step                 DecisionStep   `json:"step"`
	Status               DecisionStatus `json:"status"`
	ActorID              string         `json:"actorId"`
	AffectedRepositories []string       `json:"affectedRepositories"`
	Score                float64        `json:"score"`
	MatchedRepositories  []string       `json:"matchedRepositories,omitempty"`
	CreatedAt            time.Time      `json:"createdAt"`
}

// SimilarResult labels the recall path actually used so the frontend can
// show why these results were returned.
type SimilarResult struct {
	Mode string       `json:"mode"`
	Hits []SimilarHit `json:"hits"`
}

// ErrBadArgument marks a malformed recall request (http layer maps to 400).
var ErrBadArgument = errors.New("decisionchain: invalid argument")

// ponytail: structuralPrescreen caps the GIN prescreen before in-Go Jaccard
// ranking — ranking over the 500 newest shared-repository nodes. Per-row
// SQL-side Jaccard only if real volume ever demands it.
const structuralPrescreen = 500

func clampTopK(topK int) int {
	if topK <= 0 {
		return 5
	}
	if topK > 50 {
		return 50
	}
	return topK
}

// Similar recalls comparable past decisions. auto/semantic prefer the vector
// path (current model only, F5); provider trouble or an empty semantic
// result transparently degrades to structural with the actual mode labeled.
func (s *Service) Similar(ctx context.Context, q SimilarQuery) (SimilarResult, error) {
	if strings.TrimSpace(q.Requirement) == "" {
		return SimilarResult{}, ErrBadArgument
	}
	topK := clampTopK(q.TopK)
	mode := q.Mode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "semantic" && mode != "structural" {
		return SimilarResult{}, ErrBadArgument
	}

	if (mode == "auto" || mode == "semantic") && s.cfg.EmbeddingBaseURL != "" {
		vectors, err := s.embed(ctx, []string{NormalizeRequirement(q.Requirement)})
		if err == nil {
			hits, err := s.semanticHits(ctx, vectors[0], topK, q.MinSimilarity)
			if err == nil && len(hits) > 0 {
				return SimilarResult{Mode: "semantic", Hits: hits}, nil
			}
		}
		// provider trouble or empty result: transparent degrade (D11 tier 3)
	}
	return s.similarStructural(ctx, q, topK)
}

func (s *Service) semanticHits(ctx context.Context, query []float32, topK int, minSimilarity float64) ([]SimilarHit, error) {
	candidates, err := s.store.SemanticCandidates(ctx, s.cfg.EmbeddingModel, query, topK)
	if err != nil {
		return nil, err
	}
	hits := make([]SimilarHit, 0, len(candidates))
	for _, scored := range candidates {
		if scored.Score < minSimilarity {
			continue
		}
		hits = append(hits, nodeToHit(scored.Node, scored.Score, nil))
	}
	return hits, nil
}

func (s *Service) similarStructural(ctx context.Context, q SimilarQuery, topK int) (SimilarResult, error) {
	candidates, err := s.store.StructuralCandidates(ctx, q.RepositoryNames, structuralPrescreen)
	if err != nil {
		return SimilarResult{}, err
	}
	type ranked struct {
		hit     SimilarHit
		jaccard float64
	}
	rankedHits := make([]ranked, 0, len(candidates))
	for _, node := range candidates {
		intersection, jaccard := scoreOverlap(q.RepositoryNames, node.AffectedRepositories)
		if jaccard <= 0 || jaccard < q.MinSimilarity {
			continue
		}
		rankedHits = append(rankedHits, ranked{hit: nodeToHit(node, jaccard, intersection), jaccard: jaccard})
	}
	sort.Slice(rankedHits, func(i, j int) bool {
		if rankedHits[i].jaccard != rankedHits[j].jaccard {
			return rankedHits[i].jaccard > rankedHits[j].jaccard
		}
		return rankedHits[i].hit.CreatedAt.After(rankedHits[j].hit.CreatedAt)
	})
	if len(rankedHits) > topK {
		rankedHits = rankedHits[:topK]
	}
	hits := make([]SimilarHit, 0, len(rankedHits))
	for _, r := range rankedHits {
		hits = append(hits, r.hit)
	}
	return SimilarResult{Mode: "structural", Hits: hits}, nil
}

// SemanticSearch is the explicit full-corpus text probe. No degrade: an
// unconfigured endpoint is ErrNotConfigured (503), a provider failure is
// ErrUpstream (502) — silently returning structural hits here would mislead
// the caller about what was searched.
func (s *Service) SemanticSearch(ctx context.Context, queryText string, topK int, minSimilarity float64) ([]SimilarHit, error) {
	if strings.TrimSpace(queryText) == "" {
		return nil, ErrBadArgument
	}
	vectors, err := s.embed(ctx, []string{NormalizeRequirement(queryText)})
	if err != nil {
		return nil, err
	}
	return s.semanticHits(ctx, vectors[0], clampTopK(topK), minSimilarity)
}

// scoreOverlap returns the intersecting names and the Jaccard score
// |query ∩ repos| / |query ∪ repos| for one candidate.
func scoreOverlap(query, repos []string) (intersection []string, jaccard float64) {
	querySet := make(map[string]bool, len(query))
	for _, name := range query {
		querySet[name] = true
	}
	both := 0
	seen := map[string]bool{}
	for _, name := range repos {
		seen[name] = true
		if querySet[name] {
			both++
			intersection = append(intersection, name)
		}
	}
	union := len(querySet)
	for name := range seen {
		if !querySet[name] {
			union++
		}
	}
	if union == 0 {
		return nil, 0
	}
	return intersection, float64(both) / float64(union)
}

func nodeToHit(node DecisionNode, score float64, matched []string) SimilarHit {
	repos := node.AffectedRepositories
	if repos == nil {
		repos = []string{}
	}
	return SimilarHit{
		DecisionID:           node.ID,
		RequirementText:      node.RequirementText,
		Step:                 node.Step,
		Status:               node.Status,
		ActorID:              node.ActorID,
		AffectedRepositories: repos,
		Score:                score,
		MatchedRepositories:  matched,
		CreatedAt:            node.CreatedAt,
	}
}
