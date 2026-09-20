package observability

import "time"

// ModelCall is a product fact, without provider credentials or cloud schema.
// Optional usage remains nil when the provider never reported it.
type ModelCall struct {
	ResultStatus string `json:"result_status"`
	ResultUsed   bool   `json:"result_used"`

	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	IssueID           string    `json:"issue_id"`
	ProviderID        string    `json:"provider_id"`
	RequestedModel    string    `json:"requested_model"`
	ResponseModel     string    `json:"response_model"`
	ProviderRequestID string    `json:"provider_request_id"`
	StartedAt         time.Time `json:"started_at"`
	DurationNS        int64     `json:"duration_ns"`
	Status            string    `json:"status"`
	HTTPStatus        int       `json:"http_status"`
	InputTokens       *int64    `json:"input_tokens"`
	OutputTokens      *int64    `json:"output_tokens"`
	CacheReadTokens   *int64    `json:"cache_read_tokens"`
}
