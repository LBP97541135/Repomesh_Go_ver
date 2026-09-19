package discovery

import (
	"context"
	"encoding/json"
	"time"
)

// Stall is one stuck discovery chain, reported by the discovery domain itself
// (own schema only — the observability surface aggregates domain self-reports,
// it never queries this schema directly). kind: failed = a step block carries
// an error; stalled = mid-step with no progress past the window.
type Stall struct {
	IssueID     string    `json:"issueId"`
	IssueNumber int64     `json:"issueNumber"`
	Title       string    `json:"title"`
	Step        int       `json:"step"`
	Kind        string    `json:"kind"`
	Message     string    `json:"message"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

const stallWindow = 15 * time.Minute

// Stalls reports this project's stuck discovery chains for the observability
// alerts surface. Reads only repomesh_issues tables this service owns.
func (s *Service) Stalls(ctx context.Context, projectID string) ([]Stall, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.issue_id, i.number, i.title,
		d.analysis, d.candidates, d.classification, d.plan, d.approval, d.updated_at
		FROM repomesh_issues.issue_discoveries d
		JOIN repomesh_issues.issues i ON i.id = d.issue_id AND i.removed_at IS NULL
		WHERE d.project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Stall{}
	for rows.Next() {
		var (
			id, title string
			number    int64
			analysis  []byte
			candidate []byte
			classif   []byte
			plan      []byte
			approval  []byte
			updatedAt time.Time
		)
		if rows.Scan(&id, &number, &title, &analysis, &candidate, &classif, &plan, &approval, &updatedAt) != nil {
			continue
		}
		blocks := map[int][]byte{1: analysis, 2: candidate, 3: classif, 4: plan}
		for step, raw := range blocks {
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var block struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(raw, &block) != nil || block.Error == nil {
				continue
			}
			msg := block.Error.Message
			if msg == "" {
				msg = "步骤执行失败(未给出原因)"
			}
			out = append(out, Stall{
				IssueID: id, IssueNumber: number, Title: title,
				Step: step, Kind: "failed", Message: msg, UpdatedAt: updatedAt,
			})
		}
		// 卡住：仅「自动步该跑没跑」才算——分档审批已过而计划迟迟未生成。
		// 等人审的门(③⑤)和等选择的门(②)是产品语义，不是故障，不得告警。
		if time.Since(updatedAt) > stallWindow {
			var appr struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(approval, &appr)
			cur := currentStepOf(analysis, candidate, classif, plan)
			if cur == 4 && appr.State == "approved" {
				out = append(out, Stall{
					IssueID: id, IssueNumber: number, Title: title,
					Step: cur, Kind: "stalled",
					Message: "分档已批准但计划超过 15 分钟未生成,执行者疑似中断",
					UpdatedAt: updatedAt,
				})
			}
		}
	}
	return out, rows.Err()
}

// currentStepOf mirrors deriveStep's「首个缺失块获胜」on raw jsonb.
func currentStepOf(analysis, candidates, classification, plan []byte) int {
	present := func(raw []byte) bool { return len(raw) > 0 && string(raw) != "null" }
	switch {
	case !present(analysis):
		return 1
	case !present(candidates):
		return 2
	case !present(classification):
		return 3
	case !present(plan):
		return 4
	default:
		return 5
	}
}
