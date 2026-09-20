// Package typesafe provides the optional, project-scoped Jev verification tool.
// Model judgments are advisory and never change a test or delivery verdict.
package typesafe

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultModel    = "jev-1.13.0"
	TemplateVersion = "repomesh-evidence-v1"
	SkillCommit     = "65a39f393687675ce170e6094757de20370365b9"
	MaxBody         = 32 << 10
	MaxCalls        = 8
	CallTimeout     = 20 * time.Second
)

type Failure struct {
	Status int
	Code   string
}

func (e *Failure) Error() string         { return e.Code }
func fail(status int, code string) error { return &Failure{status, code} }

type Settings struct {
	ProjectID   string     `json:"projectId"`
	Revision    int64      `json:"revision"`
	Enabled     bool       `json:"enabled"`
	Configured  bool       `json:"configured"`
	Model       string     `json:"model"`
	CheckedAt   *time.Time `json:"checkedAt"`
	CheckStatus string     `json:"checkStatus"`
	SkillCommit string     `json:"skillCommit"`
	SkillHash   string     `json:"skillHash"`
}
type SaveInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Enabled          bool   `json:"enabled"`
	Model            string `json:"model"`
	Secret           struct {
		Mode  string `json:"mode"`
		Value string `json:"value,omitempty"`
	} `json:"secret"`
}
type Claim struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type Input struct {
	RequestID string  `json:"requestId"`
	Claims    []Claim `json:"claims"`
	Evidence  string  `json:"evidence"`
	Commit    string  `json:"commit,omitempty"`
}
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
type Evaluation struct {
	Purpose         string    `json:"purpose"`
	TaskID          string    `json:"taskId"`
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	IssueID         string    `json:"issueId"`
	RunID           string    `json:"runId"`
	RequestID       string    `json:"requestId"`
	Revision        int64     `json:"revision"`
	SkillHash       string    `json:"skillHash"`
	TemplateVersion string    `json:"templateVersion"`
	Input           Input     `json:"input"`
	InputHash       string    `json:"inputHash"`
	Status          string    `json:"status"`
	ErrorCode       string    `json:"errorCode"`
	Response        *Response `json:"response,omitempty"`
	LatencyMS       int64     `json:"latencyMs"`
	CreatedAt       time.Time `json:"createdAt"`
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var commitID = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
var secretPattern = regexp.MustCompile(`(?i)(?:apikey_[a-z0-9_]{16,}|sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9]{16,}|github_pat_[a-z0-9_]{16,}|bearer\s+[a-z0-9_.-]{16,})`)

func redact(s string) string { return secretPattern.ReplaceAllString(s, "[REDACTED]") }

func (in Input) normalized() (Input, error) {
	if !safeID.MatchString(in.RequestID) || len(in.Claims) < 1 || len(in.Claims) > 8 || len(in.Evidence) > 24<<10 || (in.Commit != "" && !commitID.MatchString(in.Commit)) {
		return Input{}, fail(422, "TYPESAFE_INVALID_INPUT")
	}
	in.Evidence = redact(strings.TrimSpace(in.Evidence))
	seen := map[string]bool{}
	claims := make([]Claim, len(in.Claims))
	for i, c := range in.Claims {
		if !safeID.MatchString(c.ID) || seen[c.ID] || strings.TrimSpace(c.Text) == "" || len(c.Text) > 1000 {
			return Input{}, fail(422, "TYPESAFE_INVALID_INPUT")
		}
		seen[c.ID] = true
		claims[i] = Claim{ID: c.ID, Text: redact(strings.TrimSpace(c.Text))}
	}
	in.Claims = claims
	raw, _ := json.Marshal(in)
	if len(raw) > MaxBody {
		return Input{}, fail(413, "TYPESAFE_INPUT_TOO_LARGE")
	}
	return in, nil
}
