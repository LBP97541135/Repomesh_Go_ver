package observeui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type EvaluationPolicy struct {
	Enabled           bool `json:"enabled"`
	DailyCallLimit    int  `json:"daily_call_limit"`
	LateWindowSeconds int  `json:"late_window_seconds"`
	AllowFixtures     bool `json:"allow_fixtures"`
}

type EvaluationClaim struct {
	PaidClaimID string `json:"paid_claim_id"`
	JudgmentID  string `json:"judgment_id"`

	ID              string    `json:"id"`
	AttemptID       string    `json:"attempt_id"`
	SourceArchive   string    `json:"source_archive"`
	TraceID         string    `json:"trace_id"`
	SubjectRevision string    `json:"subject_revision"`
	Model           string    `json:"model"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Server) loadPolicy() (EvaluationPolicy, error) {
	p := EvaluationPolicy{DailyCallLimit: 10, LateWindowSeconds: 5}
	path := filepath.Join(s.archives[0].Journal.Dir, "evaluation-policy.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8192 {
		return p, errors.New("invalid private evaluation policy")
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &p) != nil || p.DailyCallLimit < 1 || p.DailyCallLimit > 100 || p.LateWindowSeconds < 0 || p.LateWindowSeconds > 3600 {
		return p, errors.New("invalid evaluation policy")
	}
	return p, nil
}

func (s *Server) evaluationPolicy(w http.ResponseWriter, r *http.Request) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if r.Method == http.MethodPut {
		var p EvaluationPolicy
		if !readJSON(w, r, &p) {
			return
		}
		if p.DailyCallLimit < 1 || p.DailyCallLimit > 100 || p.LateWindowSeconds < 0 || p.LateWindowSeconds > 3600 {
			failHTTP(w, 400, "daily calls must be 1..100; late window 0..3600 seconds")
			return
		}
		if p.Enabled {
			// loadModel owns modelMu; read the immutable public metadata only after
			// releasing the lock to avoid a lock cycle with configuration writes.
			s.modelMu.Unlock()
			c, err := s.loadModel()
			s.modelMu.Lock()
			if err != nil || c.Provider != "typesafe" || c.Key == "" {
				failHTTP(w, 409, "Jev configuration is required for local online evaluation")
				return
			}
		}
		b, _ := json.Marshal(p)
		if err := writePrivateConfig(s.archives[0].Journal.Dir, "evaluation-policy.json", b); err != nil {
			failHTTP(w, 500, "cannot save evaluation policy")
			return
		}
		writeJSON(w, 200, p)
		return
	}
	p, err := s.loadPolicy()
	if err != nil {
		failHTTP(w, 409, err.Error())
		return
	}
	claims := []EvaluationClaim{}
	raw, err := s.archives[0].Journal.Records("evaluation_claims")
	if err != nil {
		failHTTP(w, 409, "evaluation claims unavailable")
		return
	}
	for _, b := range raw {
		var c EvaluationClaim
		if json.Unmarshal(b, &c) == nil && c.ID != "" {
			claims = append(claims, c)
		}
	}
	paidCount := 0
	paid, readErr := s.archives[0].Journal.Records("paid_attempts")
	if readErr != nil {
		failHTTP(w, 409, "paid ledger unavailable")
		return
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, raw := range paid {
		var a PaidAttempt
		if json.Unmarshal(raw, &a) == nil && a.CreatedAt.UTC().Format("2006-01-02") == today {
			paidCount++
		}
	}
	writeJSON(w, 200, map[string]any{"paid_attempt_count": paidCount, "remaining_today": max(0, p.DailyCallLimit-paidCount), "policy": p, "claims": claims, "runner": "local", "retry_policy": "unknown outcome remains reserved; manual re-evaluation only", "paid_requests": true})
}

func (s *Server) runOnline(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.onlineOnce(ctx, time.Now().UTC())
		}
	}
}

// A durable claim is written before a paid request. Lost responses or a process
// crash leave a visible reserved attempt, never an automatic double charge.
func (s *Server) onlineOnce(ctx context.Context, now time.Time) {
	s.modelMu.Lock()
	policy, err := s.loadPolicy()
	s.modelMu.Unlock()
	if err != nil || !policy.Enabled {
		return
	}
	c, err := s.loadModel()
	if err != nil || c.Provider != "typesafe" || c.Key == "" {
		return
	}
	if !s.running.TryLock() {
		return
	}
	defer s.running.Unlock()
	raw, err := s.archives[0].Journal.Records("evaluation_claims")
	if err != nil {
		return
	}
	seen := map[string]bool{}
	today := 0
	for _, b := range raw {
		var claim EvaluationClaim
		if json.Unmarshal(b, &claim) != nil {
			return
		}
		seen[claim.ID] = true
		if claim.CreatedAt.UTC().Format("2006-01-02") == now.Format("2006-01-02") {
			today++
		}
	}
	if today >= policy.DailyCallLimit {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.archives {
		a := &s.archives[i]
		spans, err := readSpans(a.Journal)
		if err != nil {
			continue
		}
		traces := map[string][]Span{}
		for _, sp := range spans {
			traces[sp.TraceID] = append(traces[sp.TraceID], sp)
		}
		for trace, group := range traces {
			latest := time.Time{}
			terminal := false
			allowed := true
			for _, sp := range group {
				if sp.End.After(latest) {
					latest = sp.End
				}
				attrs := attributes(sp.Detail)
				if str(attrs, "repomesh.execution_purpose") == "judge" {
					allowed = false
				}
				if str(attrs, "repomesh.collection_status") == "complete" && str(attrs, "repomesh.execution_status") == "completed" {
					terminal = true
				}
				kind := str(attrs, "repomesh.subject_kind")
				if !policy.AllowFixtures && !oneOfStrings(kind, "real_agent_run", "observed_product_call") {
					allowed = false
				}
			}
			if !allowed || !terminal || now.Sub(latest) < time.Duration(policy.LateWindowSeconds)*time.Second {
				continue
			}
			state, available, revision, err := s.traceMaterial(a, trace)
			if err != nil || !available["tool_trajectory"] || containsSecret(state, c.Key) {
				continue
			}
			rubric := observepipe.ObservationRubric()
			rubricJSON, _ := json.Marshal(rubric)
			id := observepipe.Digest([]byte(strings.Join([]string{trace, revision, c.Model, string(rubricJSON), "projection/3"}, "\x00")))
			if seen[id] {
				continue
			}
			// The shared evaluator reserves the durable paid attempt first.
			// A schedule record references the actual result, rather than an
			// unrelated nonce. Missing results remain visible in paid_attempts.
			s.mu.Unlock()
			result, evalErr := s.evaluateJev(ctx, a, c, "", trace, revision, state, available, nil, "")
			s.mu.Lock()
			if evalErr == nil {
				claim := EvaluationClaim{ID: id, PaidClaimID: result.ClaimID, JudgmentID: result.ID, AttemptID: result.ID, SourceArchive: a.ID, TraceID: trace, SubjectRevision: revision, Model: c.Model, CreatedAt: now}
				_ = s.archives[0].Journal.PutRecord("evaluation_claims", id, claim)
			}

			return
		}
	}
}
