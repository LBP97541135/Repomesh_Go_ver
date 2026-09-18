// Package observability implements the v1 observe read surface
// (/api/v1/observe/*) matching the frontend contract in
// frontend/src/api/contract.ts: system summary, per-issue usage, trace
// sessions/events with keyset pagination, unified logs, and alert
// rules/events with CRUD. Read models over the 0014/0025 ledger tables;
// Py observability 对等.
package observability

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- summary ----

// Summary is the GET /observe/summary payload.
type Summary struct {
	Calls           int64         `json:"calls"`
	SuccessCalls    int64         `json:"success_calls"`
	ErrorCalls      int64         `json:"error_calls"`
	SuccessRate     *float64      `json:"success_rate"`
	PromptTokens    int64         `json:"prompt_tokens"`
	CompletionToken int64         `json:"completion_tokens"`
	TotalTokens     int64         `json:"total_tokens"`
	EstimatedCost   float64       `json:"estimated_cost_usd"`
	AvgLatencyMS    *float64      `json:"avg_latency_ms"`
	LatencyP50MS    *float64      `json:"latency_p50_ms"`
	LatencyP95MS    *float64      `json:"latency_p95_ms"`
	FirstUsageAt    *time.Time    `json:"first_usage_at"`
	LastUsageAt     *time.Time    `json:"last_usage_at"`
	ByModel         []ModelMetric `json:"by_model"`
	ByStep          []StepMetric  `json:"by_step"`
	Daily           []DailyPoint  `json:"daily"`
	RecentErrors    []ErrorRow    `json:"recent_errors"`
}

type ModelMetric struct {
	Model         string  `json:"model"`
	Calls         int64   `json:"calls"`
	PromptTokens  int64   `json:"prompt_tokens"`
	CompletionTok int64   `json:"completion_tokens"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
}

type StepMetric struct {
	Step          *int  `json:"step"`
	Calls         int64 `json:"calls"`
	PromptTokens  int64 `json:"prompt_tokens"`
	CompletionTok int64 `json:"completion_tokens"`
}

type DailyPoint struct {
	Date          string `json:"date"`
	Calls         int64  `json:"calls"`
	PromptTokens  int64  `json:"prompt_tokens"`
	CompletionTok int64  `json:"completion_tokens"`
}

type ErrorRow struct {
	CreatedAt     time.Time `json:"created_at"`
	Model         string    `json:"model"`
	Operation     string    `json:"operation"`
	FinishReason  *string   `json:"finish_reason"`
	PromptTokens  int64     `json:"prompt_tokens"`
	CompletionTok int64     `json:"completion_tokens"`
	LatencyMS     *int      `json:"latency_ms"`
}

// Summary aggregates llm_usage over the trailing window (days 1..90).
func (s *Service) Summary(ctx context.Context, days int) (Summary, error) {
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	out := Summary{ByModel: []ModelMetric{}, ByStep: []StepMetric{}, Daily: []DailyPoint{}, RecentErrors: []ErrorRow{}}
	var successRate, avg, p50, p95 *float64
	err := s.pool.QueryRow(ctx, `SELECT count(*),
			count(*) FILTER (WHERE status='ok'),
			count(*) FILTER (WHERE status<>'ok'),
			COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0),
			COALESCE(sum(estimated_cost_usd),0),
			avg(duration_ms), percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms),
			percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms),
			min(created_at), max(created_at)
		FROM public.llm_usage WHERE created_at > now() - ($1 || ' days')::interval`, fmt.Sprint(days)).
		Scan(&out.Calls, &out.SuccessCalls, &out.ErrorCalls, &out.PromptTokens, &out.CompletionToken,
			&out.EstimatedCost, &avg, &p50, &p95, &out.FirstUsageAt, &out.LastUsageAt)
	if err != nil {
		return Summary{}, fmt.Errorf("observe: summary: %w", err)
	}
	if out.Calls > 0 {
		rate := float64(out.SuccessCalls) / float64(out.Calls)
		successRate = &rate
	}
	if avg != nil {
		v := *avg
		avgLatency := v
		out.AvgLatencyMS = &avgLatency
	}
	if p50 != nil {
		v := *p50
		out.LatencyP50MS = &v
	}
	if p95 != nil {
		v := *p95
		out.LatencyP95MS = &v
	}
	out.SuccessRate = successRate
	out.TotalTokens = out.PromptTokens + out.CompletionToken

	rows, err := s.pool.Query(ctx, `SELECT model, count(*), COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0), COALESCE(sum(estimated_cost_usd),0)
		FROM public.llm_usage WHERE created_at > now() - ($1 || ' days')::interval GROUP BY model ORDER BY count(*) DESC`, fmt.Sprint(days))
	if err != nil {
		return Summary{}, fmt.Errorf("observe: by_model: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m ModelMetric
		if err := rows.Scan(&m.Model, &m.Calls, &m.PromptTokens, &m.CompletionTok, &m.EstimatedCost); err != nil {
			return Summary{}, err
		}
		out.ByModel = append(out.ByModel, m)
	}

	dailyRows, err := s.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), count(*),
			COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0)
		FROM public.llm_usage WHERE created_at > now() - ($1 || ' days')::interval
		GROUP BY 1 ORDER BY 1`, fmt.Sprint(days))
	if err != nil {
		return Summary{}, fmt.Errorf("observe: daily: %w", err)
	}
	defer dailyRows.Close()
	for dailyRows.Next() {
		var p DailyPoint
		if err := dailyRows.Scan(&p.Date, &p.Calls, &p.PromptTokens, &p.CompletionTok); err != nil {
			return Summary{}, err
		}
		out.Daily = append(out.Daily, p)
	}

	errRows, err := s.pool.Query(ctx, `SELECT created_at, model, operation, finish_reason, prompt_tokens, completion_tokens, duration_ms
		FROM public.llm_usage WHERE status<>'ok' AND created_at > now() - ($1 || ' days')::interval
		ORDER BY created_at DESC LIMIT 5`, fmt.Sprint(days))
	if err != nil {
		return Summary{}, fmt.Errorf("observe: errors: %w", err)
	}
	defer errRows.Close()
	for errRows.Next() {
		var e ErrorRow
		if err := errRows.Scan(&e.CreatedAt, &e.Model, &e.Operation, &e.FinishReason, &e.PromptTokens, &e.CompletionTok, &e.LatencyMS); err != nil {
			return Summary{}, err
		}
		out.RecentErrors = append(out.RecentErrors, e)
	}
	return out, nil
}

// ---- per-issue usage ----

type IssueUsageRow struct {
	IssueID       string     `json:"issue_id"`
	Calls         int64      `json:"calls"`
	PromptTokens  int64      `json:"prompt_tokens"`
	CompletionTok int64      `json:"completion_tokens"`
	EstimatedCost float64    `json:"estimated_cost_usd"`
	AvgLatencyMS  *float64   `json:"avg_latency_ms"`
	LastUsageAt   *time.Time `json:"last_usage_at"`
}

type IssuesResponse struct {
	Issues []IssueUsageRow `json:"issues"`
}

// Issues summarizes usage per issue, most recently active first, max 100.
func (s *Service) Issues(ctx context.Context) (IssuesResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT issue_id::text, count(*), COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0),
			COALESCE(sum(estimated_cost_usd),0), avg(duration_ms), max(created_at)
		FROM public.llm_usage WHERE issue_id IS NOT NULL
		GROUP BY issue_id ORDER BY max(created_at) DESC LIMIT 100`)
	if err != nil {
		return IssuesResponse{}, fmt.Errorf("observe: issues: %w", err)
	}
	defer rows.Close()
	out := IssuesResponse{Issues: []IssueUsageRow{}}
	for rows.Next() {
		var r IssueUsageRow
		if err := rows.Scan(&r.IssueID, &r.Calls, &r.PromptTokens, &r.CompletionTok, &r.EstimatedCost, &r.AvgLatencyMS, &r.LastUsageAt); err != nil {
			return IssuesResponse{}, err
		}
		out.Issues = append(out.Issues, r)
	}
	return out, rows.Err()
}

// ---- logs ----

// LogEntryV1 mirrors the frontend LogEntry contract.
type LogEntryV1 struct {
	ID      string    `json:"id"`
	TS      time.Time `json:"ts"`
	Level   string    `json:"level"`
	Source  string    `json:"source"`
	IssueID *string   `json:"issue_id"`
	Message string    `json:"message"`
	ExcInfo *string   `json:"exc_info"`
}

type LogsResponse struct {
	Logs       []LogEntryV1 `json:"logs"`
	NextCursor *string      `json:"next_cursor"`
}

// Logs queries log entries with optional filters and keyset pagination
// ordered (logged_at DESC, id DESC); cursor is the "ts|id" of the last row.
func (s *Service) Logs(ctx context.Context, level, source, issueID, query string, limit int, cursor string) (LogsResponse, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	where := []string{}
	args := []any{}
	add := func(cond string, value ...any) {
		for _, v := range value {
			args = append(args, v)
		}
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if level != "" {
		add("level = ?", level)
	}
	if source != "" {
		add("source ILIKE '%' || ? || '%'", source)
	}
	if issueID != "" {
		add("issue_id::text = ?", issueID)
	}
	if query != "" {
		add("message ILIKE '%' || ? || '%'", query)
	}
	if cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) == 2 {
			add("ts < ?", parts[0])
		}
	}
	sqlQuery := `SELECT id::text, logged_at, level, source, issue_id::text, message, NULL::text FROM public.log_entries`
	if len(where) > 0 {
		sqlQuery += " WHERE " + strings.Join(where, " AND ")
	}
	sqlQuery += fmt.Sprintf(" ORDER BY logged_at DESC, id DESC LIMIT %d", limit)
	rows, err := s.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return LogsResponse{}, fmt.Errorf("observe: logs: %w", err)
	}
	defer rows.Close()
	out := LogsResponse{Logs: []LogEntryV1{}}
	for rows.Next() {
		var entry LogEntryV1
		if err := rows.Scan(&entry.ID, &entry.TS, &entry.Level, &entry.Source, &entry.IssueID, &entry.Message, &entry.ExcInfo); err != nil {
			return LogsResponse{}, err
		}
		out.Logs = append(out.Logs, entry)
	}
	if err := rows.Err(); err != nil {
		return LogsResponse{}, err
	}
	if len(out.Logs) == limit {
		last := out.Logs[len(out.Logs)-1]
		cursorValue := last.TS.Format(time.RFC3339Nano)
		out.NextCursor = &cursorValue
	}
	return out, nil
}

// IssueLogGroupsResponse is the log-by-issue grouping payload.
type IssueLogGroupsResponse struct {
	Issues []IssueLogGroup `json:"issues"`
}

type IssueLogGroup struct {
	IssueID string     `json:"issue_id"`
	Count   int64      `json:"count"`
	LastAt  *time.Time `json:"last_at"`
}

// LogIssueGroups groups recent logs by issue.
func (s *Service) LogIssueGroups(ctx context.Context) (IssueLogGroupsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT issue_id::text, count(*), max(logged_at)
		FROM public.log_entries WHERE issue_id IS NOT NULL
		GROUP BY issue_id ORDER BY max(logged_at) DESC LIMIT 100`)
	if err != nil {
		return IssueLogGroupsResponse{}, fmt.Errorf("observe: log groups: %w", err)
	}
	defer rows.Close()
	out := IssueLogGroupsResponse{Issues: []IssueLogGroup{}}
	for rows.Next() {
		var g IssueLogGroup
		if err := rows.Scan(&g.IssueID, &g.Count, &g.LastAt); err != nil {
			return IssueLogGroupsResponse{}, err
		}
		out.Issues = append(out.Issues, g)
	}
	return out, rows.Err()
}

// ---- alerts ----

// AlertRuleV1 mirrors the frontend AlertRule contract.
type AlertRuleV1 struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Metric        string    `json:"metric"`
	Operator      string    `json:"operator"`
	Threshold     float64   `json:"threshold"`
	WindowMinutes int       `json:"window_minutes"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AlertRulesResponse struct {
	Rules []AlertRuleV1 `json:"rules"`
}

var supportedMetrics = map[string]bool{
	"success_rate": true, "error_count": true, "latency_p95_ms": true,
	"estimated_cost_usd": true, "calls": true,
}

func validMetric(metric string) bool { return supportedMetrics[metric] }

// AlertRules lists all rules (including disabled).
func (s *Service) AlertRules(ctx context.Context) (AlertRulesResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, metric, operator, threshold_value, window_minutes, enabled, created_at, updated_at
		FROM public.alert_rules ORDER BY created_at DESC`)
	if err != nil {
		return AlertRulesResponse{}, fmt.Errorf("observe: rules: %w", err)
	}
	defer rows.Close()
	out := AlertRulesResponse{Rules: []AlertRuleV1{}}
	for rows.Next() {
		var r AlertRuleV1
		if err := rows.Scan(&r.ID, &r.Name, &r.Metric, &r.Operator, &r.Threshold, &r.WindowMinutes, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return AlertRulesResponse{}, err
		}
		out.Rules = append(out.Rules, r)
	}
	return out, rows.Err()
}

// AlertRulePayload creates or updates a rule; zero fields are ignored on PUT.
type AlertRulePayload struct {
	Name          *string  `json:"name"`
	Metric        *string  `json:"metric"`
	Operator      *string  `json:"operator"`
	Threshold     *float64 `json:"threshold"`
	WindowMinutes *int     `json:"window_minutes"`
	Enabled       *bool    `json:"enabled"`
}

// CreateAlertRule validates and inserts one rule.
func (s *Service) CreateAlertRule(ctx context.Context, payload AlertRulePayload) (AlertRuleV1, error) {
	if payload.Name == nil || payload.Metric == nil || payload.Operator == nil || payload.Threshold == nil {
		return AlertRuleV1{}, fmt.Errorf("observe: name, metric, operator and threshold are required")
	}
	if !validMetric(*payload.Metric) {
		return AlertRuleV1{}, fmt.Errorf("observe: unsupported metric %q", *payload.Metric)
	}
	if *payload.Operator != "lt" && *payload.Operator != "gt" {
		return AlertRuleV1{}, fmt.Errorf("observe: operator must be lt or gt")
	}
	window := 60
	if payload.WindowMinutes != nil {
		window = *payload.WindowMinutes
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	var rule AlertRuleV1
	err := s.pool.QueryRow(ctx, `INSERT INTO public.alert_rules (id, organization_id, name, metric, operator, threshold_value, window_minutes, enabled, created_at, updated_at)
		VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', $1, $2, $3, $4, $5, $6, now(), now())
		RETURNING id::text, name, metric, operator, threshold_value, window_minutes, enabled, created_at, updated_at`,
		*payload.Name, *payload.Metric, *payload.Operator, *payload.Threshold, window, enabled).
		Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.WindowMinutes, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return AlertRuleV1{}, fmt.Errorf("observe: create rule: %w", err)
	}
	return rule, nil
}

// UpdateAlertRule applies a partial update (PUT semantics: omitted = keep).
func (s *Service) UpdateAlertRule(ctx context.Context, ruleID string, payload AlertRulePayload) (AlertRuleV1, error) {
	if payload.Metric != nil && !validMetric(*payload.Metric) {
		return AlertRuleV1{}, fmt.Errorf("observe: unsupported metric %q", *payload.Metric)
	}
	if payload.Operator != nil && *payload.Operator != "lt" && *payload.Operator != "gt" {
		return AlertRuleV1{}, fmt.Errorf("observe: operator must be lt or gt")
	}
	sets := []string{}
	args := []any{}
	apply := func(cond string, value any) {
		args = append(args, value)
		sets = append(sets, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if payload.Name != nil {
		apply("name = ?", *payload.Name)
	}
	if payload.Metric != nil {
		apply("metric = ?", *payload.Metric)
	}
	if payload.Operator != nil {
		apply("operator = ?", *payload.Operator)
	}
	if payload.Threshold != nil {
		apply("threshold_value = ?", *payload.Threshold)
	}
	if payload.WindowMinutes != nil {
		apply("window_minutes = ?", *payload.WindowMinutes)
	}
	if payload.Enabled != nil {
		apply("enabled = ?", *payload.Enabled)
	}
	if len(sets) == 0 {
		return AlertRuleV1{}, fmt.Errorf("observe: nothing to update")
	}
	query := "UPDATE public.alert_rules SET " + strings.Join(sets, ", ") + ", updated_at=now() WHERE id=$" + fmt.Sprint(len(args)+1) +
		` RETURNING id::text, name, metric, operator, threshold_value, window_minutes, enabled, created_at, updated_at`
	args = append(args, ruleID)
	var rule AlertRuleV1
	err := s.pool.QueryRow(ctx, query, args...).
		Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.WindowMinutes, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return AlertRuleV1{}, fmt.Errorf("observe: update rule: %w", err)
	}
	return rule, nil
}

// DeleteAlertRule removes one rule and cascades its events.
func (s *Service) DeleteAlertRule(ctx context.Context, ruleID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM public.alert_rules WHERE id=$1`, ruleID)
	if err != nil {
		return fmt.Errorf("observe: delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM public.alert_events WHERE rule_id=$1`, ruleID)
	return nil
}

// AlertEventV1 mirrors the frontend AlertEvent contract.
type AlertEventV1 struct {
	ID            string     `json:"id"`
	RuleID        string     `json:"rule_id"`
	RuleName      *string    `json:"rule_name"`
	Status        string     `json:"status"`
	Message       string     `json:"message"`
	Value         float64    `json:"value"`
	WindowMinutes int        `json:"window_minutes"`
	TriggeredAt   time.Time  `json:"triggered_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
}

type AlertEventsResponse struct {
	Events []AlertEventV1 `json:"events"`
}

// AlertEvents lists firing/resolved events within the window (default 7d).
func (s *Service) AlertEvents(ctx context.Context, days int) (AlertEventsResponse, error) {
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id::text, e.rule_id::text, r.name, e.status_v1, e.message, e.value, e.window_minutes, e.fired_at, e.resolved_at
		FROM public.alert_events e LEFT JOIN public.alert_rules r ON r.id=e.rule_id
		WHERE e.fired_at > now() - ($1 || ' days')::interval
		ORDER BY e.fired_at DESC LIMIT 500`, fmt.Sprint(days))
	if err != nil {
		return AlertEventsResponse{}, fmt.Errorf("observe: events: %w", err)
	}
	defer rows.Close()
	return scanAlertEvents(rows)
}

// ActiveAlerts lists currently firing events.
func (s *Service) ActiveAlerts(ctx context.Context) (AlertEventsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id::text, e.rule_id::text, r.name, e.status_v1, e.message, e.value, e.window_minutes, e.fired_at, e.resolved_at
		FROM public.alert_events e LEFT JOIN public.alert_rules r ON r.id=e.rule_id
		WHERE e.status_v1='firing' ORDER BY e.fired_at DESC LIMIT 200`)
	if err != nil {
		return AlertEventsResponse{}, fmt.Errorf("observe: active: %w", err)
	}
	defer rows.Close()
	return scanAlertEvents(rows)
}

func scanAlertEvents(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) (AlertEventsResponse, error) {
	out := AlertEventsResponse{Events: []AlertEventV1{}}
	for rows.Next() {
		var e AlertEventV1
		if err := rows.Scan(&e.ID, &e.RuleID, &e.RuleName, &e.Status, &e.Message, &e.Value, &e.WindowMinutes, &e.TriggeredAt, &e.ResolvedAt); err != nil {
			return AlertEventsResponse{}, err
		}
		out.Events = append(out.Events, e)
	}
	return out, rows.Err()
}

// EvaluateAlerts runs one evaluation pass over all enabled rules against the
// llm_usage trailing window and records firing/resolved transitions.
func (s *Service) EvaluateAlerts(ctx context.Context) (AlertEventsResponse, error) {
	rules, err := s.AlertRules(ctx)
	if err != nil {
		return AlertEventsResponse{}, err
	}
	for _, rule := range rules.Rules {
		if !rule.Enabled {
			continue
		}
		var value float64
		query := map[string]string{
			"success_rate":       `COALESCE(avg(CASE WHEN status='ok' THEN 100.0 ELSE 0.0 END), 0)`,
			"error_count":        `count(*) FILTER (WHERE status<>'ok')`,
			"latency_p95_ms":     `COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)`,
			"estimated_cost_usd": `COALESCE(sum(estimated_cost_usd), 0)`,
			"calls":              `count(*)`,
		}[rule.Metric]
		err := s.pool.QueryRow(ctx, `SELECT `+query+` FROM public.llm_usage
			WHERE created_at > now() - ($1 || ' minutes')::interval`, fmt.Sprint(rule.WindowMinutes)).Scan(&value)
		if err != nil {
			continue
		}
		breached := (rule.Operator == "lt" && value < rule.Threshold) || (rule.Operator == "gt" && value > rule.Threshold)
		var current string
		err = s.pool.QueryRow(ctx, `SELECT status_v1 FROM public.alert_events
			WHERE rule_id=$1 AND resolved_at IS NULL ORDER BY fired_at DESC LIMIT 1`, rule.ID).Scan(&current)
		hasOpen := err == nil
		if breached && (!hasOpen || current != "firing") {
			_, _ = s.pool.Exec(ctx, `INSERT INTO public.alert_events (id, organization_id, rule_id, metric_value, message, value, window_minutes, status_v1, fired_at)
				VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', $1, '{}'::jsonb, $2, $3, $4, 'firing', now())`,
				rule.ID, fmt.Sprintf("%s %s %.4f over %dm window", rule.Metric, rule.Operator, value, rule.WindowMinutes), value, rule.WindowMinutes)
		} else if !breached && hasOpen && current == "firing" {
			_, _ = s.pool.Exec(ctx, `UPDATE public.alert_events SET status_v1='resolved', resolved_at=now()
				WHERE rule_id=$1 AND resolved_at IS NULL`, rule.ID)
		}
	}
	return s.ActiveAlerts(ctx)
}
