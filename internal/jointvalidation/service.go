// Package jointvalidation implements P1's joint_validation task type: a
// cross-repository contract verification step that depends on ≥2 repository
// tasks and compares their deliverables against the agreed interface
// document (M1 design §4, judge demo 主轴).
package jointvalidation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service runs joint validation over completed repository tasks.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ContractClause is one agreement from the interface document.
type ContractClause struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
}

// CompareCommand lists the deliverable summaries of upstream tasks and the
// contract clauses they must satisfy.
type CompareCommand struct {
	PlanID          string
	UpstreamTaskIDs []string
	Clauses         []ContractClause
}

// CompareResult reports per-clause and overall verdicts.
type CompareResult struct {
	Verdict string          `json:"verdict"` // consistent | inconsistent
	Clauses []ClauseVerdict `json:"clauses"`
}

// ClauseVerdict is one clause outcome.
type ClauseVerdict struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Found    string `json:"found,omitempty"`
	OK       bool   `json:"ok"`
}

// Compare checks each clause against the recorded deliverable summaries of
// the upstream tasks. This is the mechanical substring check the joint
// validation agent's verdict must respect; the agent can add evidence but
// never flip a clause the data contradicts.
func (s *Service) Compare(ctx context.Context, command CompareCommand) (CompareResult, error) {
	if len(command.UpstreamTaskIDs) < 2 {
		return CompareResult{}, fmt.Errorf("jointvalidation: at least two upstream tasks are required")
	}
	deliverables := map[string]string{}
	for _, taskID := range command.UpstreamTaskIDs {
		var results []byte
		if err := s.pool.QueryRow(ctx, `SELECT results FROM public.database_branch_validations
			WHERE task_id::text=$1 ORDER BY id DESC LIMIT 1`, taskID).Scan(&results); err == nil {
			var stored map[string]any
			if json.Unmarshal(results, &stored) == nil {
				for key, value := range stored {
					if text, ok := value.(string); ok {
						deliverables[key] = text
					}
				}
			}
		}
		// also scan the task's own result summary
		var summary string
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(result_summary,'') FROM public.tasks
			WHERE id::text=$1`, taskID).Scan(&summary); err == nil && summary != "" {
			deliverables["task:"+taskID] = summary
		}
	}
	result := CompareResult{Verdict: "consistent", Clauses: []ClauseVerdict{}}
	for _, clause := range command.Clauses {
		verdict := ClauseVerdict{Field: clause.Field, Expected: clause.Expected}
		if found, ok := deliverables[clause.Field]; ok && contains(found, clause.Expected) {
			verdict.Found = found
			verdict.OK = true
		} else if found != "" {
			verdict.Found = found
		}
		if !verdict.OK {
			result.Verdict = "inconsistent"
		}
		result.Clauses = append(result.Clauses, verdict)
	}
	return result, nil
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
