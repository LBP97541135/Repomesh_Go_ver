// Package observability — trace read models for the v1 observe surface
// (Py observability trace 对等): session list with keyset pagination,
// session events, cross-session events, and issue-window attribution.
package observability

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TraceSessionV1 mirrors the frontend TraceSession contract.
type TraceSessionV1 struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"session_id"`
	AgentName    string     `json:"agent_name"`
	Runtime      string     `json:"runtime"`
	SourceKey    string     `json:"source_key"`
	EventCount   int        `json:"event_count"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	ParsedAt     *time.Time `json:"parsed_at"`
	ParsingError *string    `json:"parsing_error"`
	ObjectMtime  time.Time  `json:"object_mtime"`
	ObjectSize   int64      `json:"object_size"`
}

type TraceSessionsResponse struct {
	Sessions   []TraceSessionV1 `json:"sessions"`
	NextCursor *string          `json:"next_cursor"`
}

// TraceSessions lists sessions ordered (first_seen_at DESC, id DESC) with
// keyset pagination; cursor encodes "first_seen|id" of the last row.
func (s *Service) TraceSessions(ctx context.Context, agentName, issueID string, limit int, cursor string) (TraceSessionsResponse, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	where := []string{}
	args := []any{}
	if agentName != "" {
		args = append(args, agentName)
		where = append(where, fmt.Sprintf("agent_name=$%d", len(args)))
	}
	if issueID != "" {
		// Approximate attribution: sessions overlapping the issue's activity
		// window taken from llm_usage (+/- 15 minutes).
		args = append(args, issueID)
		issueParam := fmt.Sprintf("$%d", len(args))
		args = append(args, 15)
		windowParam := fmt.Sprintf("$%d", len(args))
		where = append(where, `EXISTS (SELECT 1 FROM public.llm_usage u WHERE u.issue_id=`+issueParam+`::uuid
			AND s.first_seen_at BETWEEN u.created_at - make_interval(mins => `+windowParam+`) AND u.created_at + make_interval(mins => `+windowParam+`))`)
	}
	if cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) == 2 {
			args = append(args, parts[0])
			tsParam := fmt.Sprintf("$%d", len(args))
			args = append(args, parts[1])
			idParam := fmt.Sprintf("$%d", len(args))
			where = append(where, fmt.Sprintf("(first_seen_at, id) < (%s::timestamptz, %s::uuid)", tsParam, idParam))
		}
	}
	query := `SELECT id::text, session_id, agent_name, runtime, source_key, event_count, first_seen_at, parsed_at, parsing_error, object_mtime, object_size
		FROM public.trace_sessions s`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY first_seen_at DESC, id DESC LIMIT %d", limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return TraceSessionsResponse{}, fmt.Errorf("observe: sessions: %w", err)
	}
	defer rows.Close()
	out := TraceSessionsResponse{Sessions: []TraceSessionV1{}}
	for rows.Next() {
		var t TraceSessionV1
		if err := rows.Scan(&t.ID, &t.SessionID, &t.AgentName, &t.Runtime, &t.SourceKey, &t.EventCount, &t.FirstSeenAt, &t.ParsedAt, &t.ParsingError, &t.ObjectMtime, &t.ObjectSize); err != nil {
			return TraceSessionsResponse{}, err
		}
		out.Sessions = append(out.Sessions, t)
	}
	if err := rows.Err(); err != nil {
		return TraceSessionsResponse{}, err
	}
	if len(out.Sessions) == limit {
		last := out.Sessions[len(out.Sessions)-1]
		cursorValue := last.FirstSeenAt.Format(time.RFC3339Nano) + "|" + last.ID
		out.NextCursor = &cursorValue
	}
	return out, nil
}

// TraceEventV1 mirrors the frontend TraceEvent contract.
type TraceEventV1 struct {
	ID        string                 `json:"id"`
	SessionID string                 `json:"session_id"`
	Seq       int                    `json:"seq"`
	TS        time.Time              `json:"ts"`
	EventType string                 `json:"event_type"`
	Name      string                 `json:"name"`
	Role      *string                `json:"role"`
	Summary   *string                `json:"summary"`
	TokenCnt  *int                   `json:"token_count"`
	LatencyMS *int                   `json:"latency_ms"`
	Status    string                 `json:"status"`
	Payload   map[string]interface{} `json:"payload"`
	AgentName *string                `json:"agent_name"`
	SessionEx *string                `json:"session_external_id"`
}

type TraceEventsV1Response struct {
	Events     []TraceEventV1 `json:"events"`
	NextSeq    *int           `json:"next_seq"`
	NextCursor *string        `json:"next_cursor"`
}

// TraceSessionEvents returns one session's events ordered by seq ascending.
func (s *Service) TraceSessionEvents(ctx context.Context, sessionRowID string, limit, afterSeq int) (TraceEventsV1Response, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	query := `SELECT e.id::text, e.session_id::text, e.seq, e.ts, e.event_type, e.name, e.role, e.summary, e.token_count, e.latency_ms, e.status, e.payload, s.agent_name, s.session_id
		FROM public.trace_events e JOIN public.trace_sessions s ON s.id=e.session_id
		WHERE e.session_id::text=$1`
	args := []any{sessionRowID}
	if afterSeq > 0 {
		args = append(args, afterSeq)
		query += fmt.Sprintf(" AND e.seq > $%d", len(args))
	}
	query += fmt.Sprintf(" ORDER BY e.seq LIMIT %d", limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return TraceEventsV1Response{}, fmt.Errorf("observe: session events: %w", err)
	}
	defer rows.Close()
	return scanTraceEvents(rows, true)
}

// TraceEvents lists events across sessions ordered (ts DESC, id DESC).
func (s *Service) TraceEvents(ctx context.Context, eventType, status, agentName string, limit int, cursor string) (TraceEventsV1Response, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	where := []string{}
	args := []any{}
	if eventType != "" {
		args = append(args, eventType)
		where = append(where, fmt.Sprintf("e.event_type=$%d", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("e.status=$%d", len(args)))
	}
	if agentName != "" {
		args = append(args, agentName)
		where = append(where, fmt.Sprintf("s.agent_name=$%d", len(args)))
	}
	if cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) == 2 {
			args = append(args, parts[0])
			tsParam := fmt.Sprintf("$%d", len(args))
			args = append(args, parts[1])
			idParam := fmt.Sprintf("$%d", len(args))
			where = append(where, fmt.Sprintf("(e.ts, e.id) < (%s::timestamptz, %s::uuid)", tsParam, idParam))
		}
	}
	query := `SELECT e.id::text, e.session_id::text, e.seq, e.ts, e.event_type, e.name, e.role, e.summary, e.token_count, e.latency_ms, e.status, e.payload, s.agent_name, s.session_id
		FROM public.trace_events e JOIN public.trace_sessions s ON s.id=e.session_id`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY e.ts DESC, e.id DESC LIMIT %d", limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return TraceEventsV1Response{}, fmt.Errorf("observe: events: %w", err)
	}
	defer rows.Close()
	out, err := scanTraceEvents(rows, false)
	if err != nil {
		return TraceEventsV1Response{}, err
	}
	if len(out.Events) == limit {
		last := out.Events[len(out.Events)-1]
		cursorValue := last.TS.Format(time.RFC3339Nano) + "|" + last.ID
		out.NextCursor = &cursorValue
	}
	return out, nil
}

func scanTraceEvents(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, asc bool) (TraceEventsV1Response, error) {
	out := TraceEventsV1Response{Events: []TraceEventV1{}}
	for rows.Next() {
		var e TraceEventV1
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Seq, &e.TS, &e.EventType, &e.Name, &e.Role, &e.Summary, &e.TokenCnt, &e.LatencyMS, &e.Status, &e.Payload, &e.AgentName, &e.SessionEx); err != nil {
			return TraceEventsV1Response{}, err
		}
		out.Events = append(out.Events, e)
	}
	if asc && len(out.Events) > 0 {
		next := out.Events[len(out.Events)-1].Seq
		out.NextSeq = &next
	}
	return out, rows.Err()
}

// TraceIssueGroup is one row of the trace-by-issue attribution view.
type TraceIssueGroup struct {
	IssueID         string     `json:"issue_id"`
	ActivityStart   *time.Time `json:"activity_start"`
	ActivityEnd     *time.Time `json:"activity_end"`
	SuspectedSessns int        `json:"suspected_sessions"`
	LastSessionAt   *time.Time `json:"last_session_at"`
}

type TraceIssueGroupsResponse struct {
	Issues []TraceIssueGroup `json:"issues"`
}

// TraceIssueGroups approximates which sessions belong to which issue: the
// issue activity window comes from llm_usage, sessions overlap it within 15
// minutes on either side.
func (s *Service) TraceIssueGroups(ctx context.Context) (TraceIssueGroupsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.issue_id::text, min(u.created_at), max(u.created_at),
			(SELECT count(*) FROM public.trace_sessions s WHERE s.first_seen_at BETWEEN min(u.created_at) - interval '15 minutes' AND max(u.created_at) + interval '15 minutes'),
			(SELECT max(s.first_seen_at) FROM public.trace_sessions s WHERE s.first_seen_at BETWEEN min(u.created_at) - interval '15 minutes' AND max(u.created_at) + interval '15 minutes')
		FROM public.llm_usage u WHERE u.issue_id IS NOT NULL GROUP BY u.issue_id ORDER BY max(u.created_at) DESC LIMIT 100`)
	if err != nil {
		return TraceIssueGroupsResponse{}, fmt.Errorf("observe: trace groups: %w", err)
	}
	defer rows.Close()
	out := TraceIssueGroupsResponse{Issues: []TraceIssueGroup{}}
	for rows.Next() {
		var g TraceIssueGroup
		if err := rows.Scan(&g.IssueID, &g.ActivityStart, &g.ActivityEnd, &g.SuspectedSessns, &g.LastSessionAt); err != nil {
			return TraceIssueGroupsResponse{}, err
		}
		out.Issues = append(out.Issues, g)
	}
	return out, rows.Err()
}
