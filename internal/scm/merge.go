package scm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PullMerger 是合并所需的最小 GitHub 能力（只要两个方法）。
//
// 为什么用接口而不是直接依赖 github.Client：scm 不该知道 App 凭据怎么装、
// 令牌怎么铸；它只需要"给我一个能合并的手"。测试里也好替。
type PullMerger interface {
	InstallationAccessToken(ctx context.Context, owner, name string) (string, time.Time, error)
	MergePullRequest(ctx context.Context, token, owner, name string, number int, commitTitle string) error
}

// MergeOutcome 是合并的结果（界面直接显示这句话）。
type MergeOutcome struct {
	ChangeSetID string `json:"changeSetId"`
	Merged      bool   `json:"merged"`
	PR          string `json:"pr"`
	Message     string `json:"message"`
}

// Merge 真的把 PR 合掉（交付段的收口动作）。
//
// 三道门，一道都不能省：
//  1. change set 必须**属于这个项目**（跨项目 id 一律 404 语义，不泄漏）；
//  2. 必须有 PR（pr_url 里带 owner/repo/number），没有就如实说没有；
//  3. **合并闸门必须开着**（fail-closed：pushed/pr/ci/reviewed 四项都由流水线
//     真实事件记录，缺一项就不许合）—— 闸门是设计里的安全边界，不是可跳过的提示。
//
// 幂等：已经 merged 的直接返回成功，不重复调 GitHub。
func (s *Service) Merge(ctx context.Context, projectID, changeSetID, actor string, merger PullMerger) (MergeOutcome, error) {
	var prURL, status string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(cs.pr_url, ''), cs.status
		FROM public.change_sets cs
		WHERE cs.id = $1::uuid
		  AND EXISTS (SELECT 1 FROM public.tasks t WHERE t.id = cs.task_id AND t.project_id = $2::uuid)`,
		changeSetID, projectID).Scan(&prURL, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeOutcome{}, fmt.Errorf("scm: change set 不在这个项目里")
	}
	if err != nil {
		return MergeOutcome{}, fmt.Errorf("scm: load change set: %w", err)
	}
	if status == "merged" {
		return MergeOutcome{ChangeSetID: changeSetID, Merged: true, PR: prURL, Message: "这条已经合并过了（幂等，未重复调用 GitHub）"}, nil
	}
	owner, repo, number, ok := parsePullURL(prURL)
	if !ok {
		return MergeOutcome{}, fmt.Errorf("scm: 这条 change set 还没有 PR（pr_url=%q），先让执行面开 PR 再合并", prURL)
	}
	gate, err := s.Gate(ctx, changeSetID)
	if err != nil {
		return MergeOutcome{}, err
	}
	if !gate.Open {
		return MergeOutcome{}, fmt.Errorf(
			"scm: 合并闸门未开（push=%v PR=%v CI=%v 评审=%v）——缺的那几项要由流水线真实事件补齐，不能在界面上跳过",
			gate.Pushed, gate.PR, gate.CIPassed, gate.Reviewed)
	}
	if merger == nil {
		return MergeOutcome{}, fmt.Errorf("scm: 服务端没有配置可用的 GitHub 凭据，无法合并")
	}
	token, _, err := merger.InstallationAccessToken(ctx, owner, repo)
	if err != nil {
		return MergeOutcome{}, fmt.Errorf("scm: 铸该仓库的 installation token 失败：%w", err)
	}
	if err := merger.MergePullRequest(ctx, token, owner, repo, number, "RepoMesh delivery "+changeSetID); err != nil {
		return MergeOutcome{}, fmt.Errorf("scm: 合并被 GitHub 拒绝：%w", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE public.change_sets SET status='merged' WHERE id=$1::uuid`, changeSetID); err != nil {
		return MergeOutcome{}, fmt.Errorf("scm: mark merged: %w", err)
	}
	if err := s.RecordEvent(ctx, changeSetID, "merge", `{"actor":"`+actor+`","pr":"`+prURL+`"}`); err != nil {
		return MergeOutcome{}, err
	}
	return MergeOutcome{ChangeSetID: changeSetID, Merged: true, PR: prURL, Message: "已合并 " + prURL}, nil
}

// parsePullURL 从 PR 链接里取出 owner / repo / number。
//
// 认不出来就返回 ok=false —— 不猜编号，也不把别的链接当成 PR（猜错会把
// 一个不相干的 PR 合掉，这是唯一会真动用户仓库的地方，宁可拒绝）。
func parsePullURL(raw string) (owner, repo string, number int, ok bool) {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(raw), "/"), "/")
	index := -1
	for i, part := range parts {
		if part == "pull" {
			index = i
			break
		}
	}
	if index < 2 || index+1 >= len(parts) {
		return "", "", 0, false
	}
	owner, repo = parts[index-2], parts[index-1]
	n, err := strconv.Atoi(parts[index+1])
	if err != nil || n <= 0 || owner == "" || repo == "" {
		return "", "", 0, false
	}
	return owner, repo, n, true
}

// ChangeSetForTask 返回该任务名下的 change set（没有就返回空串，不报错）。
//
// 用途：经理批准任务时要往它的 change set 上记一条 review —— 闸门四项里的
// 最后一项，也是唯一由**人**产生的那一项。
func (s *Service) ChangeSetForTask(ctx context.Context, taskID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM public.change_sets
		WHERE task_id = $1::uuid ORDER BY version DESC LIMIT 1`, taskID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("scm: change set by task: %w", err)
	}
	return id, nil
}
