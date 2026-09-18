package scan

import (
	"context"
	"sort"
	"strings"
)

// The scope flow (design doc §8): the user finalizes a repository set, the
// completeness checker verifies it against the graph before anything is
// committed, and the submission boundary refuses sets the catalog cannot
// vouch for. The checker only advises — the user's submission is final.

// NoSuggestions is the manual-mode suggester: it never suggests. Assembly
// wires it whenever the 辅助选仓 switch is off (or no engine is installed).
type NoSuggestions struct{}

func (NoSuggestions) Suggest(string, int) ([]Suggestion, error) { return nil, nil }

// UnknownRepositoriesError rejects a scope that names repositories the
// catalog does not know. The selection UI lists catalog rows only, so this
// fires for API direct calls or when the catalog changed between picking and
// submitting (a repository archived meanwhile).
type UnknownRepositoriesError struct{ IDs []string }

func (e *UnknownRepositoriesError) Error() string {
	return "unknown repositories in scope: " + strings.Join(e.IDs, ", ")
}

// MissingDependency is a repository outside the selection that selected
// repositories depend on. Confirmed evidence makes it a strong suggestion
// (the build/execution chain is broken without it); declared evidence is a
// hint only.
type MissingDependency struct {
	RepositoryID    string        `json:"repositoryId"`
	Name            string        `json:"name"`
	Confidence      Confidence    `json:"confidence"`
	EvidenceSources []string      `json:"evidenceSources"`
	Evidence        []DepEvidence `json:"evidence"`
}

// AffectedRepository is a repository outside the selection that depends on a
// selected one — the blast radius of the work, planning context only.
type AffectedRepository struct {
	RepositoryID string   `json:"repositoryId"`
	Name         string   `json:"name"`
	Via          []string `json:"via"` // selected repositories it depends on
}

// CompletenessReport is the checker's advisory output. Collisions surfaces
// alias conflicts from the graph build: edges touching a conflicted name may
// point at the wrong repository, and operators should see that.
type CompletenessReport struct {
	Missing    []MissingDependency  `json:"missing"`
	Affected   []AffectedRepository `json:"affected"`
	Collisions []string             `json:"collisions"`
}

// CheckCompleteness verifies a selection against the graph over the whole
// catalog (the graph needs every scanned repository, not just the selection,
// to resolve edges in both directions).
func CheckCompleteness(ctx context.Context, store CatalogStore, registry AliasRegistry, selectedIDs []string) (CompletenessReport, error) {
	cards, err := store.List(ctx)
	if err != nil {
		return CompletenessReport{}, err
	}

	byID := make(map[string]RepositoryCard, len(cards))
	for _, card := range cards {
		byID[card.ID] = card
	}
	var unknown []string
	for _, id := range selectedIDs {
		if _, ok := byID[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return CompletenessReport{}, &UnknownRepositoriesError{IDs: unknown}
	}

	selected := map[string]bool{}
	for _, id := range selectedIDs {
		selected[id] = true
	}

	graph := BuildGraph(cards, registry)
	missing := map[string]*MissingDependency{}
	affected := map[string]*AffectedRepository{}

	for _, edge := range graph.Edges {
		fromSelected, toSelected := selected[edge.FromID], selected[edge.ToID]
		switch {
		case fromSelected && !toSelected:
			entry := missing[edge.ToID]
			if entry == nil {
				entry = &MissingDependency{
					RepositoryID: edge.ToID,
					Name:         edge.ToName,
					Confidence:   edge.Confidence,
				}
				missing[edge.ToID] = entry
			}
			sourceName := edge.FromID
			if card, ok := byID[edge.FromID]; ok {
				sourceName = card.Name
			}
			entry.EvidenceSources = appendUnique(entry.EvidenceSources, sourceName)
			entry.Evidence = appendUniqueEvidence(entry.Evidence, DepEvidence{
				Name: edge.ToName, Mechanism: edge.Mechanism, Confidence: edge.Confidence,
			})
			entry.Confidence = stronger(entry.Confidence, edge.Confidence)
		case !fromSelected && toSelected:
			entry := affected[edge.FromID]
			if entry == nil {
				entry = &AffectedRepository{RepositoryID: edge.FromID}
				if card, ok := byID[edge.FromID]; ok {
					entry.Name = card.Name
				}
				affected[edge.FromID] = entry
			}
			toName := edge.ToName
			if card, ok := byID[edge.ToID]; ok {
				toName = card.Name
			}
			entry.Via = appendUnique(entry.Via, toName)
		}
	}

	report := CompletenessReport{Collisions: registry.Collisions}
	for _, entry := range missing {
		sort.Strings(entry.EvidenceSources)
		report.Missing = append(report.Missing, *entry)
	}
	for _, entry := range affected {
		sort.Strings(entry.Via)
		report.Affected = append(report.Affected, *entry)
	}
	sort.Slice(report.Missing, func(i, j int) bool {
		return report.Missing[i].Name < report.Missing[j].Name
	})
	sort.Slice(report.Affected, func(i, j int) bool {
		return report.Affected[i].Name < report.Affected[j].Name
	})
	return report, nil
}

// SubmitScope is the submission boundary: it validates the selection against
// the catalog and, when valid, hands the final set to the accept callback.
// The checker only advises; this is where the user's decision becomes final.
func SubmitScope(ctx context.Context, store CatalogStore, selectedIDs []string, accept func([]string) error) error {
	cards, err := store.List(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(cards))
	for _, card := range cards {
		known[card.ID] = true
	}
	var unknown []string
	for _, id := range selectedIDs {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return &UnknownRepositoriesError{IDs: unknown}
	}
	return accept(selectedIDs)
}

// stronger returns the higher-confidence grade: confirmed evidence outranks
// declared when both point at the same missing repository.
func stronger(a, b Confidence) Confidence {
	if a == ConfidenceConfirmed || b == ConfidenceConfirmed {
		return ConfidenceConfirmed
	}
	return ConfidenceDeclared
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueEvidence(values []DepEvidence, value DepEvidence) []DepEvidence {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
