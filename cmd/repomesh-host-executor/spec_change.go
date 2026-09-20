package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"repomesh.local/repomesh/internal/humancontrol"
	"repomesh.local/repomesh/internal/spec"
)

// ───────────── A2：执行中的 agent 只能"提"规格变更请求 ─────────────
//
// 用户裁定：**不允许 agent 自己改 spec 生效**。worker 在任务工作区写出
// spec-change-request.json；host executor 在 run 退出后把它**收走**：
//   · 落库（待批，spec 域自己的 control_operations）；
//   · 落审核台（checkpoint=spec-change），人在这里批。
// 批准之后才升版并触发重规划 —— 那条路在 web 侧（审核台的 Decide）。
//
// 收不到产物就什么都不做：不编一份"agent 想改规格"的请求。

// specReviewSink 把 spec 域的审核单请求折成 humancontrol 的命令。
type specReviewSink struct{ desk *humancontrol.Service }

func (s specReviewSink) RequestChangeReview(ctx context.Context, command spec.ChangeReviewCommand) (string, error) {
	view, err := s.desk.Request(ctx, humancontrol.RequestCommand{
		ProjectID:       command.ProjectID,
		Checkpoint:      spec.ChangeCheckpoint,
		EvidenceVersion: command.EvidenceVersion,
		Title:           command.Title,
		Summary:         command.Summary,
		RequestedBy:     command.RequestedBy,
		IssueID:         command.IssueID,
		Origin:          command.Origin,
		Assignee:        command.Assignee,
	})
	if err != nil {
		return "", err
	}
	return view.ID, nil
}

// readSpecChangeRequest 读回并解析产物。读不到/不合格一律 ok=false ——
// 与 test-evidence 同一条规矩：宁可界面什么都不显示，也不编一份请求。
//
// 两处都找：单点 run 的命令是 `cd repo` 之后跑的，agent 按提示词写下的文件会落在
// <workspace>/repo/ 下（2026-09-20 实测，与 test-evidence.json 同一个坑）。
func readSpecChangeRequest(workspace string) (spec.SpecChangeRequest, bool) {
	var raw []byte
	var err error
	for _, candidate := range []string{
		filepath.Join(workspace, spec.ChangeRequestFile),
		filepath.Join(workspace, "repo", spec.ChangeRequestFile),
	} {
		raw, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return spec.SpecChangeRequest{}, false
	}
	request, err := spec.ParseChangeRequest(raw)
	if err != nil {
		return spec.SpecChangeRequest{}, false
	}
	return request, true
}

// recordSpecChangeRequest 收下这次 run 提出的规格变更请求（如果有）。
func (e *executor) recordSpecChangeRequest(ctx context.Context, runID, taskRef, workspace string) {
	request, ok := readSpecChangeRequest(workspace)
	if !ok {
		return
	}
	var projectID, issueID, owner string
	if err := e.pool.QueryRow(ctx, `SELECT t.project_id::text,
		   COALESCE(scope.issue_id,''), COALESCE(p.owner,'')
		FROM public.tasks t
		LEFT JOIN public.task_repository_scopes scope ON scope.task_id = t.id
		LEFT JOIN repomesh_projects.projects p ON p.id = t.project_id::text
		WHERE t.id = $1::uuid`, taskRef).Scan(&projectID, &issueID, &owner); err != nil {
		return
	}
	if projectID == "" {
		return
	}
	if issueID != "" && request.IssueID == "" {
		request.IssueID = issueID
	}
	if request.RunID == "" {
		request.RunID = runID
	}
	if request.TaskID == "" {
		request.TaskID = taskRef
	}
	service := spec.New(e.pool).WithReviews(specReviewSink{desk: humancontrol.New(e.pool)})
	submission, err := service.SubmitChangeRequest(ctx, projectID, "agent_worker", owner, request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "executor: 规格变更请求收下失败 run=%s: %v\n", runID, err)
		return
	}
	// 房间里说一声（人要知道这条任务提了规格变更，以及它现在挂在审核台）。
	body := "本任务提出**规格变更请求**（" + request.Repository + "）：" + request.Reason +
		"；已挂到审核台等待人工批准" + map[bool]string{true: "（重放，未重复建单）", false: ""}[submission.Duplicate] + "。"
	e.recordRoomMessage(ctx, taskRef, "agent_worker", body)
}
