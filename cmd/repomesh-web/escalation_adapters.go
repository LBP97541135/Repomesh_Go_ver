package main

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/decisionchain"
	"repomesh.local/repomesh/internal/scan"
	"repomesh.local/repomesh/internal/tasks"
)

// 本文件是 A1「动态引入新仓库」的地基：把 internal/tasks 的升级梯（escalation
// ladder）接到真实的数据源上。
//
// 2026-09-20 实测：这条链路此前**整条没接** —— /plans/{id}/interrupt 是个空壳
// （解出 body 就丢掉、回一个 interrupt_accepted），而它背后那套
// tasks.EscalationService（InterruptPlanRepo / triggerOnboarding / GateReady /
// DropRepos）除测试外没有任何调用方。本文件补的是它需要的三个端口。

// escalationSink 把升级梯事件写进决策链。
//
// 决策链那一侧**已经有落库实现**（decisionchain.PostgresStore 的
// RecordFeedback / BlockedSince / RecordPlanRevisedInTx），这里补的只是
// tasks 侧事件 → decisionchain.Event 的字段映射 —— 不重复实现落库逻辑，
// 也不另起一套表（决策链是这套系统唯一的决策审计面）。
type escalationSink struct {
	store *decisionchain.PostgresStore
}

// actorTypeOf 把升级梯的角色映到决策链的 actor_type。
// human 是真人在系统里操作；worker / tm / leader 都是 agent 侧的产出。
func actorTypeOf(role tasks.FeedbackRole) string {
	if role == tasks.RoleHuman {
		return "human"
	}
	return "llm"
}

// statusOf 把升级梯的跳（blocked / closed）映到决策链状态。
func statusOf(status string) decisionchain.DecisionStatus {
	if status == "closed" {
		return decisionchain.StatusClosed
	}
	return decisionchain.StatusBlocked
}

func (s escalationSink) RecordFeedback(ctx context.Context, e tasks.FeedbackEvent) (string, time.Time, error) {
	node, err := s.store.RecordFeedback(ctx, decisionchain.Event{
		Requirement:    e.RequirementText,
		Actor:          e.ReporterID,
		ActorType:      actorTypeOf(e.Role),
		IdempotencyKey: e.IdempotencyKey,
		Step:           decisionchain.StepConfirmation,
		Status:         statusOf(e.Status),
		UpstreamRef:    e.UpstreamRef,
		Action:         "升级梯上报（" + string(e.Role) + "）",
		Rationale:      e.Reason,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return node.ID, node.CreatedAt, nil
}

func (s escalationSink) BlockedSince(ctx context.Context, requirementKey string, since time.Time) ([]tasks.BlockedFeedback, error) {
	nodes, err := s.store.BlockedSince(ctx, requirementKey, since)
	if err != nil {
		return nil, err
	}
	out := make([]tasks.BlockedFeedback, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, tasks.BlockedFeedback{
			NodeID:               node.ID,
			ReporterID:           node.ActorID,
			Role:                 node.ActorType,
			Reason:               node.Rationale,
			UpstreamRef:          node.ParentNodeID,
			AffectedRepositories: node.AffectedRepositories,
			CreatedAt:            node.CreatedAt,
		})
	}
	return out, nil
}

// RecordPlanRevised 在**计划换代的那个事务里**落一条决策单（协议 §2.4：计划轴与
// 决策轴同事务提交，要么同时生效要么同时回滚）。
func (s escalationSink) RecordPlanRevised(ctx context.Context, tx pgx.Tx, e tasks.PlanRevisedEvent) error {
	_, err := s.store.RecordPlanRevisedInTx(ctx, tx, decisionchain.Event{
		Requirement:    e.RequirementText,
		Actor:          e.Actor,
		ActorType:      "human",
		IdempotencyKey: e.IdempotencyKey,
		Step:           decisionchain.StepTask,
		Status:         decisionchain.StatusAdjusted,
		UpstreamRef:    e.UpstreamRef,
		Action:         "计划重排",
		Rationale:      e.Reason,
	})
	return err
}

// escalationCatalog 是升级梯的就绪判定来源：这个仓库有没有完成扫描（AutoCard 落库）。
//
// 判定口径与 discovery 的仓库名片取法一致：按 url 精确比对（带不带 .git、带不带
// 尾斜杠都算同一把 URL），不做模糊匹配 —— 模糊匹配会把同名的另一个仓库认成它。
type escalationCatalog struct {
	pool *pgxpool.Pool
}

func (c escalationCatalog) Ready(ctx context.Context, name string) (bool, error) {
	owner, repo, found := strings.Cut(strings.TrimSpace(name), "/")
	if !found || owner == "" || repo == "" {
		return false, nil
	}
	var ready bool
	query := "SELECT EXISTS (SELECT 1 FROM repomesh_scan.repositories" +
		" WHERE profiled_at IS NOT NULL" +
		"   AND lower(regexp_replace(rtrim(url, '/'), '\\.git$', '')) IN (" +
		"         lower('https://github.com/' || $1 || '/' || $2)," +
		"         lower('http://github.com/'  || $1 || '/' || $2)," +
		"         lower('git@github.com:'     || $1 || '/' || $2)))"
	err := c.pool.QueryRow(ctx, query, owner, repo).Scan(&ready)
	return ready, err
}

// escalationAdjacency 是升级梯**判定步骤**的依赖邻接视图。
//
// 2026-09-20：此前 Adjacency 为 nil，而 nil 的语义是"判定按无邻接处理（仅 X 本身）"
// —— 于是新增仓库**永远不会被判为影响当前计划**，这条能力等于空转。这里接上扫描域
// 的依赖图：BuildAliasRegistry + BuildGraph，边来自各仓库名片里**观测到的运行时调用**
// （ObservedCalls）。证据认不出的名字不产生边 —— "未知"是诚实数据，不猜。
type escalationAdjacency struct {
	pool *pgxpool.Pool
}

func (a escalationAdjacency) Neighbors(ctx context.Context, name string) ([]string, []string, error) {
	catalog := scan.NewPostgresCatalog(a.pool)
	cards, err := catalog.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	registry := scan.BuildAliasRegistry(cards)
	target, ok := registry.Resolve(name)
	if !ok {
		// 认不出这个仓库：不编邻接，如实返回空（调用方按"无邻接"处理）。
		return nil, nil, nil
	}
	byID := map[string]string{}
	for _, card := range cards {
		byID[card.ID] = card.Name
	}
	graph := scan.BuildGraph(cards, registry)
	dependsOn := []string{}
	dependedBy := []string{}
	for _, edge := range graph.Edges {
		if edge.FromID == target.ID {
			dependsOn = append(dependsOn, edge.ToName)
		}
		if edge.ToID == target.ID {
			if from, found := byID[edge.FromID]; found {
				dependedBy = append(dependedBy, from)
			}
		}
	}
	return dependsOn, dependedBy, nil
}
