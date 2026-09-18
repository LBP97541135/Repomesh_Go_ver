// Package observability implements M9's minimal read surface over the
// already-migrated ledger tables (0014 runtime ledger): trace session/event
// queries and log lookup (Py observability 对等最小切片). No new tables; no
// ingestion here.
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service is the read-side facade.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// TraceSessionSummary is one trace session row (0014 trace_sessions).
type TraceSessionSummary struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId"`
	AgentName  string     `json:"agentName"`
	Runtime    string     `json:"runtime"`
	EventCount int        `json:"eventCount"`
	FirstSeen  time.Time  `json:"firstSeen"`
	ParsedAt   *time.Time `json:"parsedAt"`
}

// ListTraceSessions returns recent trace sessions, newest first.
func (s *Service) ListTraceSessions(ctx context.Context, limit int) ([]TraceSessionSummary, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, session_id, agent_name, runtime, event_count, first_seen_at, parsed_at
		FROM public.trace_sessions ORDER BY first_seen_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("observability: trace query failed: %w", err)
	}
	defer rows.Close()
	result := []TraceSessionSummary{}
	for rows.Next() {
		var item TraceSessionSummary
		if rows.Scan(&item.ID, &item.SessionID, &item.AgentName, &item.Runtime, &item.EventCount, &item.FirstSeen, &item.ParsedAt) != nil {
			return nil, fmt.Errorf("observability: trace scan failed")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// TraceEvent is one event inside a trace session.
type TraceEvent struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Seq       int             `json:"seq"`
	Kind      string          `json:"kind"`
	Timestamp *time.Time      `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// ListTraceEvents returns the events of one session in sequence order.
func (s *Service) ListTraceEvents(ctx context.Context, sessionID string, limit int) ([]TraceEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, session_id::text, seq, event_type, ts, payload
		FROM public.trace_events WHERE session_id::text=$1 ORDER BY seq LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("observability: event query failed: %w", err)
	}
	defer rows.Close()
	result := []TraceEvent{}
	for rows.Next() {
		var item TraceEvent
		if rows.Scan(&item.ID, &item.SessionID, &item.Seq, &item.Kind, &item.Timestamp, &item.Payload) != nil {
			return nil, fmt.Errorf("observability: event scan failed")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// LogEntry is one projected log row (0014 log_entries).
type LogEntry struct {
	ID       string    `json:"id"`
	Level    string    `json:"level"`
	Source   string    `json:"source"`
	Message  string    `json:"message"`
	LoggedAt time.Time `json:"loggedAt"`
}

// ListLogs returns recent log entries, optionally filtered by level.
func (s *Service) ListLogs(ctx context.Context, level string, limit int) ([]LogEntry, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	query := `SELECT id::text, level, source, message, logged_at FROM public.log_entries`
	args := []any{}
	if level != "" {
		query += ` WHERE level=$1`
		args = append(args, level)
	}
	query += fmt.Sprintf(` ORDER BY logged_at DESC LIMIT %d`, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("observability: log query failed: %w", err)
	}
	defer rows.Close()
	result := []LogEntry{}
	for rows.Next() {
		var item LogEntry
		if rows.Scan(&item.ID, &item.Level, &item.Source, &item.Message, &item.LoggedAt) != nil {
			return nil, fmt.Errorf("observability: log scan failed")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// AlertRule is one alert rule row (0014 alert_rules).
type AlertRule struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Metric    string          `json:"metric"`
	Threshold json.RawMessage `json:"threshold"`
	Enabled   bool            `json:"enabled"`
}

// ListAlertRules returns the alert rules of one organization.
func (s *Service) ListAlertRules(ctx context.Context, organizationID string) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, metric, threshold, enabled
		FROM public.alert_rules WHERE organization_id::text=$1 ORDER BY created_at`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("observability: alert rule query failed: %w", err)
	}
	defer rows.Close()
	result := []AlertRule{}
	for rows.Next() {
		var item AlertRule
		if rows.Scan(&item.ID, &item.Name, &item.Metric, &item.Threshold, &item.Enabled) != nil {
			return nil, fmt.Errorf("observability: alert rule scan failed")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// AlertEvent is one fired alert (0014 alert_events).
type AlertEvent struct {
	ID       string     `json:"id"`
	RuleID   string     `json:"ruleId"`
	Message  string     `json:"message"`
	Status   string     `json:"status"`
	FiredAt  time.Time  `json:"firedAt"`
	Resolved *time.Time `json:"resolvedAt"`
}

// ListAlertEvents returns recent alert events, optionally open-only.
func (s *Service) ListAlertEvents(ctx context.Context, organizationID string, openOnly bool, limit int) ([]AlertEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	query := `SELECT id::text, rule_id::text, message, status, fired_at, resolved_at
		FROM public.alert_events WHERE organization_id::text=$1`
	args := []any{organizationID}
	if openOnly {
		query += ` AND resolved_at IS NULL`
	}
	query += fmt.Sprintf(` ORDER BY fired_at DESC LIMIT %d`, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("observability: alert event query failed: %w", err)
	}
	defer rows.Close()
	result := []AlertEvent{}
	for rows.Next() {
		var item AlertEvent
		if rows.Scan(&item.ID, &item.RuleID, &item.Message, &item.Status, &item.FiredAt, &item.Resolved) != nil {
			return nil, fmt.Errorf("observability: alert event scan failed")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// UsageSummary aggregates model/token cost totals from the llm_usage ledger
// (0014) for one organization (Py observability usage_query 对等).
type UsageSummary struct {
	OrganizationID string `json:"organizationId"`
	Requests       int64  `json:"requests"`
	InputTokens    int64  `json:"inputTokens"`
	OutputTokens   int64  `json:"outputTokens"`
}

// UsageSummary computes the totals; a missing ledger simply yields zeros.
func (s *Service) UsageSummary(ctx context.Context, organizationID string) (UsageSummary, error) {
	summary := UsageSummary{OrganizationID: organizationID}
	var requests, inputTokens, outputTokens *int64
	err := s.pool.QueryRow(ctx, `SELECT count(*), SUM(prompt_tokens), SUM(completion_tokens)
		FROM public.llm_usage WHERE organization_id::text=$1`, organizationID).
		Scan(&requests, &inputTokens, &outputTokens)
	if err != nil {
		// table exists since 0014; treat absence of rows as zero totals
		summary.Requests, summary.InputTokens, summary.OutputTokens = 0, 0, 0
		return summary, nil
	}
	if requests != nil {
		summary.Requests = *requests
	}
	if inputTokens != nil {
		summary.InputTokens = *inputTokens
	}
	if outputTokens != nil {
		summary.OutputTokens = *outputTokens
	}
	return summary, nil
}
