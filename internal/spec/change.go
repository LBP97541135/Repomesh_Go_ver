package spec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ───────────── A2：agent 只能"提"规格变更请求，人批才生效 ─────────────
//
// 用户裁定：**不允许 agent 自己改 spec 生效**。执行中的 worker 在任务工作区写出
// 产物 spec-change-request.json；它只被**收走**（落审核台 + 决策链），不被应用。
// 人在审核台上批准之后，才以请求内容落**下一版**规格并批准（升版），再触发重规划。
//
// 为什么请求体存在 control_operations（action='spec-change-request'）而不是新表：
// 这张表本来就是 spec 域的落库位（规格本身也以 action='specification' 存这里，
// 幂等槽 + JSON 载荷），请求与规格同域同形，读面不必再学一套。

const (
	// ChangeRequestFile 是 worker 必须写出的产物文件名（工作区根下）。
	ChangeRequestFile = "spec-change-request.json"
	// ChangeCheckpoint 是审核台上这类请求的卡点名。
	ChangeCheckpoint = "spec-change"
	changeAction     = "spec-change-request"
)

// SpecChangeRequest 是执行中 agent 提出的规格变更请求（产物形状）。
type SpecChangeRequest struct {
	// IssueID 是这份规格变更请求的**归属**（规格以 issue 为单位）。
	IssueID string `json:"issue_id,omitempty"`
	// Repository 只作上下文（这一版主要动哪个仓库），不参与身份。
	Repository string `json:"repository,omitempty"`
	Title      string `json:"title"`
	Reason     string `json:"reason"`
	Changes    string `json:"changes"`
	Evidence   string `json:"evidence,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	RunID      string `json:"run_id,omitempty"`
}

// ParseChangeRequest 校验产物形状（**结构性**校验，不替 agent 判断内容）。
// 写不出合格产物就是不合格 —— 没有兜底模拟。
func ParseChangeRequest(raw []byte) (SpecChangeRequest, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return SpecChangeRequest{}, fmt.Errorf("spec: 规格变更请求是空文件")
	}
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	var request SpecChangeRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(trimmed)), &request); err != nil {
		return SpecChangeRequest{}, fmt.Errorf("spec: 规格变更请求不是合法 JSON：%w", err)
	}
	// 仓库不再是必填（规格以 issue 为单位）：只要求"改成什么"与"为什么"。
	if strings.TrimSpace(request.Changes) == "" {
		return SpecChangeRequest{}, fmt.Errorf("spec: 规格变更请求缺少 changes（要改成什么）")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return SpecChangeRequest{}, fmt.Errorf("spec: 规格变更请求缺少 reason（为什么现有规格不够）")
	}
	return request, nil
}

// EvidenceVersion 是这类请求的幂等键：同一次请求重放不该在审核台堆出一排待审
// （humancontrol.Request 按 (project, checkpoint, evidence_version) 去重）。
func (r SpecChangeRequest) EvidenceVersion() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		r.Repository, r.Title, r.Reason, r.Changes, r.Evidence, r.RunID,
	}, "|")))
	return hex.EncodeToString(sum[:16])
}

// ChangeReviewCommand 是落审核单所需的事实（组合根把它折成 humancontrol.RequestCommand）。
type ChangeReviewCommand struct {
	ProjectID       string
	EvidenceVersion string
	Title           string
	Summary         string
	Repository      string
	RequestedBy     string
	IssueID         string
	Origin          string
	Assignee        string
}

// ReviewSink 是审核台的写面。组合根接 humancontrol.Service。
type ReviewSink interface {
	RequestChangeReview(ctx context.Context, command ChangeReviewCommand) (string, error)
}

// Submission 是一次"已收下、待人工批准"的结果。
type Submission struct {
	ReviewID        string `json:"reviewId"`
	EvidenceVersion string `json:"evidenceVersion"`
	// Duplicate：同一次请求此前已经收过（幂等重放），没有新建审核单。
	Duplicate bool `json:"duplicate"`
}

// AppliedChange 是人批之后真的升了版的结果。
type AppliedChange struct {
	SpecID     string `json:"specId"`
	Version    int    `json:"version"`
	Repository string `json:"repository"`
}

// SubmitChangeRequest 收下 agent 的规格变更请求：落库（待批）+ 落审核台。
// **不应用变更** —— 应用只发生在 ApproveChange（人批）里。
func (s *Service) SubmitChangeRequest(ctx context.Context, projectID, requestedBy, assignee string, request SpecChangeRequest) (Submission, error) {
	if strings.TrimSpace(projectID) == "" {
		return Submission{}, fmt.Errorf("spec: project is required")
	}
	evidence := request.EvidenceVersion()
	existing, err := s.pendingChangeRequest(ctx, projectID, evidence)
	if err != nil {
		return Submission{}, err
	}
	if existing != "" {
		// 幂等：同一次请求重放不再堆一条待批、也不新建审核单。
		reviewID := ""
		if s.reviews != nil {
			reviewID, err = s.reviews.RequestChangeReview(ctx, s.changeReviewCommand(projectID, requestedBy, assignee, request, evidence))
			if err != nil {
				return Submission{}, err
			}
		}
		return Submission{ReviewID: reviewID, EvidenceVersion: evidence, Duplicate: true}, nil
	}
	id, err := newID("spreq_")
	if err != nil {
		return Submission{}, err
	}
	payload := map[string]any{
		"repository": request.Repository, "title": request.Title, "reason": request.Reason,
		"changes": request.Changes, "evidence": request.Evidence,
		"task_id": request.TaskID, "run_id": request.RunID, "issue_id": request.IssueID,
		"requested_by": requestedBy, "evidence_version": evidence, "state": "pending",
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input)
		VALUES ($1,$2,$3,1,$4)`, id, projectID, changeAction, mustJSON(payload)); err != nil {
		return Submission{}, fmt.Errorf("spec: 规格变更请求落库失败：%w", err)
	}
	reviewID := ""
	if s.reviews != nil {
		reviewID, err = s.reviews.RequestChangeReview(ctx, s.changeReviewCommand(projectID, requestedBy, assignee, request, evidence))
		if err != nil {
			return Submission{}, err
		}
	}
	return Submission{ReviewID: reviewID, EvidenceVersion: evidence}, nil
}

// ApproveChange 人批准之后：以请求内容落**下一版**规格并批准（升版）。
// 返回新版本号与仓库，供调用方触发重规划；请求本身标记为已批。
func (s *Service) ApproveChange(ctx context.Context, projectID, approverID, evidenceVersion string) (AppliedChange, error) {
	row, request, found, err := s.loadChangeRequest(ctx, projectID, evidenceVersion)
	if err != nil {
		return AppliedChange{}, err
	}
	if !found {
		return AppliedChange{}, fmt.Errorf("spec: 找不到待批的规格变更请求（evidence=%s）", evidenceVersion)
	}
	// 以请求内容落**下一版**规格并批准：作者是**人**（批准者），出处记进 origin。
	// agent 只是"提"的人，规格生效这一步必须留下人的 id。
	created, err := s.Create(ctx, CreateCommand{
		ProjectID:  projectID,
		IssueID:    request.IssueID,
		Repository: request.Repository,
		AuthorID:   approverID,
		Title:      request.Title,
		Content:    request.Changes,
		Origin:     "spec-change-request:" + evidenceVersion,
	})
	if err != nil {
		return AppliedChange{}, err
	}
	approved, err := s.Approve(ctx, created.ID, approverID)
	if err != nil {
		return AppliedChange{}, err
	}
	// 标记请求已批：状态写在 result 列（canonical_input 是 bytea，不能就地做 jsonb 运算）。
	marked := map[string]any{}
	for key, value := range row.Payload {
		marked[key] = value
	}
	marked["state"] = "approved"
	marked["approved_by"] = approverID
	marked["approved_version"] = approved.Version
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_messages.control_operations
		SET result=$2 WHERE project_id=$1 AND action=$3 AND command_slot_id=$4`,
		projectID, mustJSON(marked), changeAction, row.ID); err != nil {
		return AppliedChange{}, fmt.Errorf("spec: 变更请求标记失败：%w", err)
	}
	return AppliedChange{SpecID: approved.ID, Version: approved.Version, Repository: request.Repository}, nil
}

func (s *Service) changeReviewCommand(projectID, requestedBy, assignee string, request SpecChangeRequest, evidence string) ChangeReviewCommand {
	summary := strings.TrimSpace(request.Reason)
	if strings.TrimSpace(request.Changes) != "" {
		summary = summary + "\n\n要改成：" + strings.TrimSpace(request.Changes)
	}
	return ChangeReviewCommand{
		ProjectID: projectID, EvidenceVersion: evidence,
		Title: "规格变更请求 · " + request.Repository, Summary: summary,
		Repository: request.Repository, RequestedBy: requestedBy,
		IssueID: request.IssueID, Origin: "spec-change-request", Assignee: assignee,
	}
}

// changeRow 是 control_operations 里一条规格变更请求的读面形状。
//
// 2026-09-20 实测：canonical_input 是 **bytea**，不是 jsonb —— 在 SQL 里对它做
// ->> 会直接报 42883（operator does not exist: bytea ->> unknown）。载荷在 Go 侧
// 解，与 Current/NextVersion 同一读法；状态写在 result 列（规格那边也是这么存的）。
type changeRow struct {
	ID      string
	Payload map[string]any
}

func (s *Service) changeRows(ctx context.Context, projectID string) ([]changeRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT command_slot_id, canonical_input, result
		FROM repomesh_messages.control_operations WHERE project_id=$1 AND action=$2`,
		projectID, changeAction)
	if err != nil {
		return nil, fmt.Errorf("spec: 变更请求查询失败：%w", err)
	}
	defer rows.Close()
	out := []changeRow{}
	for rows.Next() {
		var id string
		var canonical, result []byte
		if err := rows.Scan(&id, &canonical, &result); err != nil {
			return nil, fmt.Errorf("spec: 变更请求扫描失败：%w", err)
		}
		source := canonical
		if len(result) > 0 {
			source = result
		}
		var payload map[string]any
		if json.Unmarshal(source, &payload) != nil {
			continue
		}
		out = append(out, changeRow{ID: id, Payload: payload})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spec: 变更请求读行失败：%w", err)
	}
	return out, nil
}

func (s *Service) pendingChangeRequest(ctx context.Context, projectID, evidence string) (string, error) {
	rows, err := s.changeRows(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if toString(row.Payload["evidence_version"]) == evidence && toString(row.Payload["state"]) == "pending" {
			return row.ID, nil
		}
	}
	return "", nil
}

func (s *Service) loadChangeRequest(ctx context.Context, projectID, evidence string) (changeRow, SpecChangeRequest, bool, error) {
	rows, err := s.changeRows(ctx, projectID)
	if err != nil {
		return changeRow{}, SpecChangeRequest{}, false, err
	}
	for _, row := range rows {
		if toString(row.Payload["evidence_version"]) != evidence {
			continue
		}
		if toString(row.Payload["state"]) != "pending" {
			// 已批/已撤的请求不再是"待批" —— 重复批准必须失败，不能悄悄再升一版。
			return row, SpecChangeRequest{}, false, nil
		}
		payload := row.Payload
		return row, SpecChangeRequest{
			Repository: toString(payload["repository"]), Title: toString(payload["title"]),
			Reason: toString(payload["reason"]), Changes: toString(payload["changes"]),
			Evidence: toString(payload["evidence"]), TaskID: toString(payload["task_id"]),
			RunID: toString(payload["run_id"]), IssueID: toString(payload["issue_id"]),
		}, true, nil
	}
	return changeRow{}, SpecChangeRequest{}, false, nil
}
