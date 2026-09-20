package discovery

import (
	"context"
	"fmt"
	"time"
)

// ReopenEmptyCandidates 把"一个仓库都没评上"的 ② 候选评分打回重做。
//
// 为什么需要它（2026-09-20 线上实测）：
//
//	需求里没点名仓库时，② 的仓库池此前**只取本 issue 已确认的范围** —— 范围是空的，
//	池子就是空的，候选因此一个人都没评（items 为空）。③ 分档随之把"空"分成全排除，
//	⑤ 审批于是永远以 ErrNoRepositories 失败，自动托管每 10 秒撞一次墙，界面上这条
//	issue 永远停在原地。
//
// 池子的口径已经修好（loadRepoPool 现在会退到本项目的全部仓库目录），但这些**修好
// 之前**留下的候选还冻在"空"上。这里只做一件很窄的事：
//
//	候选为空（真的没人评过） **且** 本项目现在确实有仓库可评 → 清掉候选与它下游的
//	分档、审批，让下一拍重新派发 ②。
//
// 三条护栏，一条都不省：
//   - items 非空（真的评过）就**不动** —— 绝不拿"重跑"覆盖人的/agent 的判断；
//   - 项目里还是没有仓库就**不动** —— 重跑只会再得一个空，纯空转；
//   - 幂等键只放行一次 —— 同一条 issue 不会反复重开（记录进 idempotency ledger）。
//
// 计划与物化**不动**：它们是下游产物，可能已经被人看过、甚至已经在跑任务。
func (s *Service) ReopenEmptyCandidates(ctx context.Context, issueID, agentID, idempotencyKey string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return false, err
	}
	if st == nil || st.Candidates == nil {
		return false, nil
	}
	if _, ok := replay(st, idempotencyKey); ok {
		// 已经重开过一次：不再重开（避免"空候选 → 重开 → 还是空 → 再重开"的环）。
		return false, nil
	}
	items, _ := st.Candidates["items"].([]any)
	if len(items) > 0 {
		return false, nil
	}
	var pool int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.project_repositories WHERE project_id=$1`,
		st.ProjectID).Scan(&pool); err != nil {
		return false, err
	}
	if pool == 0 {
		return false, nil
	}
	reason := fmt.Sprintf("候选为空（评分时仓库池还是空的），而本项目现有 %d 个仓库可评：打回 ② 重做", pool)
	st.Candidates = nil
	st.Classification = nil
	st.Approval = nil
	st.EvidenceVersion = nil
	st.EffectiveTiers = nil
	recordReceipt(st, idempotencyKey, map[string]any{
		"task_id": nil, "step": PlanningCandidates, "status": "reopened",
		"by_agent_id": agentID, "reason": reason, "pool_size": pool, "ran_at": time.Now().UTC(),
	})
	if err := s.save(ctx, tx, st); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
