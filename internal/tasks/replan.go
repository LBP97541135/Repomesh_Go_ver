package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ───────────── 重排 v2 的落库入口（A1(d)：动态引入新仓库）─────────────
//
// 协议 §2 步骤 3-5：收集窗开完之后由 Leader 产出 v2。2026-09-20 审计发现这条链
// **没有消费者** —— MarkDeprecated 把 plans.replan_state 置成 'deprecated'，
// 全仓却没有一处把它变成新版本。人工打断于是停在一句"已开启收集窗"上：决策单落了、
// 受影响集合回了，DAG 永远长不出新节点，新仓库进了库却不进计划。
//
// 这里补的是那个消费者。产物由 **Leader（仓库领导）agent** 在收集窗的受影响集合上
// 产出（发现链的第 6 步，见 internal/discovery），落库走 ApplyRevision —— 全量快照
// 替换 + 任务轴迁移 + 双轴挂钩同事务。**不允许 agent 自己改计划生效**：产物先落
// 发现链快照与决策链，再经 ApplyRevision 的乐观版本校验进 public.plans。

// DeriveBatches 把仓库级依赖图折成批次：第 n 批 = 依赖全部落在更早批次里的仓库。
//
// 依赖方向与 ValidateBatches 的读法一致：dag[仓库] = 它依赖的仓库。
// 依赖成环时**不猜**：环成员既不会被拆开，也不会让整张图静默失败 —— 它们按名字
// 排序整批落在最后，并原样回报给调用方（Replan 据此拒绝落库）。
func DeriveBatches(repos []string, dag map[string][]string) ([][]string, []string) {
	unique := []string{}
	seen := map[string]bool{}
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		unique = append(unique, repo)
	}
	sort.Strings(unique)

	remaining := map[string]bool{}
	for _, repo := range unique {
		remaining[repo] = true
	}
	batches := [][]string{}
	for len(remaining) > 0 {
		layer := []string{}
		for _, repo := range unique {
			if !remaining[repo] {
				continue
			}
			ready := true
			for _, dep := range dag[repo] {
				dep = strings.TrimSpace(dep)
				if dep == "" || dep == repo || !remaining[dep] {
					continue // 自环与图外依赖不阻塞分层（ValidateBatches 会另行校验）
				}
				ready = false
				break
			}
			if ready {
				layer = append(layer, repo)
			}
		}
		if len(layer) == 0 {
			cycle := []string{}
			for _, repo := range unique {
				if remaining[repo] {
					cycle = append(cycle, repo)
				}
			}
			batches = append(batches, cycle)
			return batches, cycle
		}
		for _, repo := range layer {
			delete(remaining, repo)
		}
		batches = append(batches, layer)
	}
	return batches, nil
}

// ReplanCommand 是一次 v2 重排的落库请求：仓库清单 + 任务全集 + 仓库级依赖。
// Batches 留空时按 DAG 推导。
type ReplanCommand struct {
	PlanID         string
	Actor          string
	Reason         string
	UpstreamRef    string
	IdempotencyKey string
	Repositories   []string
	Tasks          []TaskSnapshot
	DAG            map[string][]string
	Batches        [][]string
}

// Replan 把一次重排落成 v2（读当前版本 → 定批次 → 幂等 → ApplyRevision）。
//
// 三道**机械**前置校验，都不替 agent 做业务判断：
//   - 依赖成环 → 拒绝，环成员原样写进错误（不把环抹平成"看起来能跑"的计划）；
//   - 批次里的仓库在 v2 任务集里没有任务 → 拒绝：快照替换会把没被认领的 v1 任务
//     置 superseded，少一个仓库的任务就等于把那个仓库从计划里悄悄删掉；
//   - 同键不同含义 → 拒绝（Python 侧的 meaning-change guard，保持同一语义）。
func (p *PostgresStore) Replan(ctx context.Context, cmd ReplanCommand) (PlanRevision, error) {
	if strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return PlanRevision{}, fmt.Errorf("%w: idempotency key is required", ErrConflict)
	}
	plan, err := p.GetPlan(ctx, cmd.PlanID)
	if err != nil {
		return PlanRevision{}, err
	}
	batches := cmd.Batches
	if len(batches) == 0 {
		derived, cycles := DeriveBatches(cmd.Repositories, cmd.DAG)
		if len(cycles) > 0 {
			return PlanRevision{}, fmt.Errorf("%w: 重排后的仓库依赖成环：%s",
				ErrInvalidPlan, strings.Join(cycles, "、"))
		}
		batches = derived
	}
	if missing := repositoriesWithoutTask(batches, cmd.Tasks); len(missing) > 0 {
		return PlanRevision{}, fmt.Errorf("%w: 这些仓库在 v2 里没有任务：%s",
			ErrInvalidPlan, strings.Join(missing, "、"))
	}
	// 幂等重放：同键同含义返回**已提交的那一版**，不再动任务轴。
	//
	// 为什么不能借 ApplyRevision 的重放分支：那里比对的是 ExpectedVersion，而重放时
	// 计划的当前版本已经是 v2 了（v1→v2 已在首次提交时发生）—— 直接转过去必然被判成
	// "这个键的含义变了"，同一次重排的重放于是永远失败。这里先认历史，再谈换代。
	history, err := p.PlanRevisions(ctx, cmd.PlanID)
	if err != nil {
		return PlanRevision{}, err
	}
	for _, entry := range history {
		if entry.IdempotencyKey != cmd.IdempotencyKey {
			continue
		}
		if !sameReplanMeaning(entry, cmd, batches) {
			return PlanRevision{}, fmt.Errorf("%w: revision idempotency key changed meaning", ErrConflict)
		}
		return entry, nil
	}
	return p.ApplyRevision(ctx, RevisionCommand{
		PlanID:          cmd.PlanID,
		ExpectedVersion: plan.PlanVersion,
		NewBatches:      batches,
		NewTasks:        cmd.Tasks,
		DAG:             cmd.DAG,
		Actor:           cmd.Actor,
		Reason:          cmd.Reason,
		UpstreamRef:     cmd.UpstreamRef,
		IdempotencyKey:  cmd.IdempotencyKey,
	})
}

// sameReplanMeaning 判"同键是否同含义"：actor / reason / 批次 / 任务全集一致才算重放。
func sameReplanMeaning(entry PlanRevision, cmd ReplanCommand, batches [][]string) bool {
	if entry.Actor != cmd.Actor || entry.Reason != cmd.Reason {
		return false
	}
	entryBatches, _ := jsonMarshal(entry.NewBatches)
	newBatches, _ := jsonMarshal(batches)
	if string(entryBatches) != string(newBatches) {
		return false
	}
	entryTasks, _ := jsonMarshal(entry.NewTasks)
	newTasks, _ := jsonMarshal(cmd.Tasks)
	return string(entryTasks) == string(newTasks)
}

// repositoriesWithoutTask 返回批次里**一个任务都没有**的仓库（保序、去重）。
func repositoriesWithoutTask(batches [][]string, tasks []TaskSnapshot) []string {
	covered := map[string]bool{}
	for _, task := range tasks {
		covered[task.RepositoryID] = true
	}
	missing := []string{}
	seen := map[string]bool{}
	for _, batch := range batches {
		for _, repo := range batch {
			if covered[repo] || seen[repo] {
				continue
			}
			seen[repo] = true
			missing = append(missing, repo)
		}
	}
	return missing
}
