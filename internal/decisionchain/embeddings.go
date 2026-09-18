package decisionchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrNotConfigured: semantic recall is off (no embedding endpoint).
	ErrNotConfigured = errors.New("decisionchain: embedding endpoint is not configured")
	// ErrUpstream: the embedding provider failed; semantic-search surfaces
	// this as 502 (no silent degrade on the explicit probe — D11/§5.3).
	ErrUpstream = errors.New("decisionchain: embedding provider failed")
)

// embeddingDims is the storage contract of decision_embeddings.embedding_vec
// (D13): 1024-dim bge-m3-class vectors; mismatched responses fail loudly.
const embeddingDims = 1024

const embeddingTimeout = 30 * time.Second

const defaultEmbeddingBatch = 16

// EmbedText builds the deterministic embedding input of a decision node —
// the same node must always yield the same request (Python _text_for parity).
func EmbedText(node DecisionNode) string {
	return strings.Join([]string{
		string(node.Step),
		string(node.Status),
		strings.Join(node.AffectedRepositories, " "),
		node.RequirementText,
	}, " | ")
}

// RefreshResult honestly reports what a refresh batch did; a failed item
// carries its reason instead of being silently skipped.
type RefreshResult struct {
	Refreshed int    `json:"refreshed"`
	Failed    int    `json:"failed"`
	Reason    string `json:"reason,omitempty"`
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// embed calls the OpenAI-compatible /embeddings endpoint once for the whole
// batch. No configuration → ErrNotConfigured; transport or API failure →
// ErrUpstream. Never called on the record path (D11: 写路径永不调 LLM).
func (s *Service) embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if s.cfg.EmbeddingBaseURL == "" {
		return nil, ErrNotConfigured
	}
	payload, err := json.Marshal(embeddingsRequest{Model: s.cfg.EmbeddingModel, Input: inputs})
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, embeddingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		strings.TrimRight(s.cfg.EmbeddingBaseURL, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.EmbeddingAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.EmbeddingAPIKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrUpstream, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrUpstream, resp.StatusCode,
			strings.TrimSpace(string(body)))
	}
	var decoded embeddingsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUpstream, err)
	}
	if len(decoded.Data) != len(inputs) {
		return nil, fmt.Errorf("%w: got %d embeddings for %d inputs",
			ErrUpstream, len(decoded.Data), len(inputs))
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			return nil, fmt.Errorf("%w: embedding index %d out of range", ErrUpstream, item.Index)
		}
		vec := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vec[i] = float32(v)
		}
		vectors[item.Index] = vec
	}
	for i, vec := range vectors {
		if vec == nil {
			return nil, fmt.Errorf("%w: missing embedding for input %d", ErrUpstream, i)
		}
	}
	return vectors, nil
}

// Refresh embeds and stores up to batch pending nodes. Nodes never embedded
// or embedded with another model (F5) are re-embedded. Unconfigured endpoint
// honestly returns zero counts; per-item dimension mismatches count as
// failed with the reason attached.
func (s *Service) Refresh(ctx context.Context, batch int) (RefreshResult, error) {
	if s.cfg.EmbeddingBaseURL == "" {
		return RefreshResult{}, nil
	}
	if batch <= 0 {
		batch = defaultEmbeddingBatch
	}
	pending, err := s.store.PendingEmbeddings(ctx, s.cfg.EmbeddingModel, batch)
	if err != nil {
		return RefreshResult{}, err
	}
	result := RefreshResult{}
	if len(pending) == 0 {
		return result, nil
	}
	inputs := make([]string, len(pending))
	for i, node := range pending {
		inputs[i] = EmbedText(node)
	}
	vectors, err := s.embed(ctx, inputs)
	if err != nil {
		return RefreshResult{Failed: len(pending), Reason: err.Error()}, nil
	}
	for i, node := range pending {
		if len(vectors[i]) != embeddingDims {
			result.Failed++
			result.Reason = fmt.Sprintf("node %s: dimension %d != %d (model %q)",
				node.ID, len(vectors[i]), embeddingDims, s.cfg.EmbeddingModel)
			continue
		}
		if err := s.store.UpsertEmbedding(ctx, node.ID, s.cfg.EmbeddingModel, vectors[i], time.Now()); err != nil {
			result.Failed++
			result.Reason = err.Error()
			continue
		}
		result.Refreshed++
	}
	return result, nil
}
