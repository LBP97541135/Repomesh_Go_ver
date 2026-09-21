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

// Failure 是 scm 的领域错误：它带一句**能直接给用户看**的话。
//
// 2026-09-21 用户报「合并失败不告诉缺哪一门」：Merge 本来就已经算出了
// 「闸门未开（push=… PR=… CI=… 评审=…）」，但它此前是 fmt.Errorf 的**裸错误**，
// web 层的 writeProjectError 认不出类型，统一降级成
// 503 RESULT_UNCONFIRMED + 写死的 "The request could not be completed." ——
// 那句可行动的真话只进了服务端日志，用户只看到一个 503。
//
// 与 projects.Failure / issues.Failure 同一套分层惯例：领域包定义自己的错误，
// web 层在共享出口识别一次。Message 只放**有意写给用户看的领域说明**，
// 绝不放底层错误原文（那可能带内部细节）。
type Failure struct {
	Status  int
	Code    string
	Message string
	// Cause 只进服务端日志，**不进响应体** —— 与 issues.Failure.Cause 同一取向：
	// 底层错误原文可能带内部细节，不该出现在给用户的那句话里。
	Cause error
}

func (e *Failure) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Message + " (cause: " + e.Cause.Error() + ")"
	}
	return e.Code + ": " + e.Message
}

// mergeGateMessage 把闸门状态翻成「缺哪一门」的人话。
//
// 用户 2026-09-21 报的正是这一点：只说"闸门没开"等于没说 —— 要能一眼看出
// 该去补哪一项，而不是拿着 503 猜。
func mergeGateMessage(gate MergeGate) string {
	missing := []string{}
	if !gate.Pushed {
		missing = append(missing, "push（分支还没推上去）")
	}
	if !gate.PR {
		missing = append(missing, "PR（还没开 PR）")
	}
	if !gate.CIPassed {
		missing = append(missing, "CI（检查还没通过）")
	}
	if !gate.Reviewed {
		missing = append(missing, "评审（还没有评审记录）")
	}
	if len(missing) == 0 {
		// 四项都齐却判未开 —— 不该发生。如实说，不编一个"缺 X"出来。
		return "合并闸门判为未开，但 push / PR / CI / 评审四项都已是真值 —— 闸门判定与状态不一致，请记录这条去排查。"
	}
	return "合并闸门未开，缺：" + strings.Join(missing, "、") +
		"。这几项要由流水线真实事件补齐，不能在界面上跳过。"
}

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
		return MergeOutcome{}, &Failure{Status: 404, Code: "RESOURCE_NOT_FOUND",
			Message: "这条 change set 不在当前项目里。"}
	}
	if err != nil {
		return MergeOutcome{}, fmt.Errorf("scm: load change set: %w", err)
	}
	if status == "merged" {
		return MergeOutcome{ChangeSetID: changeSetID, Merged: true, PR: prURL, Message: "这条已经合并过了（幂等，未重复调用 GitHub）"}, nil
	}
	owner, repo, number, ok := parsePullURL(prURL)
	if !ok {
		return MergeOutcome{}, &Failure{Status: 409, Code: "PR_MISSING",
			Message: "这条 change set 还没有 PR，先让执行面开出 PR 再合并。"}
	}
	gate, err := s.Gate(ctx, changeSetID)
	if err != nil {
		return MergeOutcome{}, err
	}
	if !gate.Open {
		return MergeOutcome{}, &Failure{Status: 409, Code: "MERGE_GATE_CLOSED", Message: mergeGateMessage(gate)}
	}
	if merger == nil {
		return MergeOutcome{}, &Failure{Status: 503, Code: "GITHUB_CREDENTIALS_MISSING",
			Message: "服务端没有配置可用的 GitHub 凭据，无法合并。"}
	}
	token, _, err := merger.InstallationAccessToken(ctx, owner, repo)
	if err != nil {
		return MergeOutcome{}, &Failure{Status: 502, Code: "GITHUB_TOKEN_FAILED",
			Message: "铸该仓库的 installation token 失败，合并没有执行。", Cause: err}
	}
	if err := merger.MergePullRequest(ctx, token, owner, repo, number, "RepoMesh delivery "+changeSetID); err != nil {
		return MergeOutcome{}, &Failure{Status: 502, Code: "MERGE_REJECTED",
			Message: "GitHub 拒绝了这次合并（可能是冲突或分支保护）。", Cause: err}
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
