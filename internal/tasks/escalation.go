package tasks

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// 升级梯子（协议 §2.1-2.3）：反馈统一落决策链（不建独立表）——
// worker 节点 → TM 升级节点（upstream 指 worker 节点）→ Leader 重规划节点
// （upstream 指 TM 节点）。梯子每一跳可溯；消化 = closed 节点。

// ErrUnknownPlan reports an escalation against a plan that does not exist.
var ErrUnknownPlan = errors.New("tasks: plan does not exist")

// ErrEmptyRepo reports a human interrupt without a repository name.
var ErrEmptyRepo = errors.New("tasks: repository name is required")

// FeedbackRole is who filed the feedback on the escalation ladder.
type FeedbackRole string

const (
	RoleWorker FeedbackRole = "worker"
	RoleTM     FeedbackRole = "tm"
	RoleLeader FeedbackRole = "leader"
	// RoleHuman：执行中人工打断（协议 §3 人类驱动变体的执行中变体）。
	// 人是升级梯子的顶端——无需 Worker/TM 过滤，直接进收集窗。
	RoleHuman FeedbackRole = "human"
)

// FeedbackEvent is one ladder hop: a blocked report (or a TM close).
type FeedbackEvent struct {
	RequirementText      string
	RequirementKey       string
	ReporterID           string
	Role                 FeedbackRole
	Reason               string
	AffectedRepositories []string
	UpstreamRef          string // 升级时指向上一跳节点；首跳为空
	Status               string // "blocked"（上报/升级）| "closed"（TM 消化）
	IdempotencyKey       string
}

// BlockedFeedback is one collected ladder leaf after the window.
type BlockedFeedback struct {
	NodeID               string    `json:"nodeId"`
	ReporterID           string    `json:"reporterId"`
	Role                 string    `json:"role"`
	Reason               string    `json:"reason"`
	UpstreamRef          string    `json:"upstreamRef,omitempty"`
	AffectedRepositories []string  `json:"affectedRepositories"`
	CreatedAt            time.Time `json:"createdAt"`
}

// DecisionSink gains the feedback ports: RecordFeedback (blocked/closed hop)
// and BlockedSince (window collection — ladder leaves only).
type DecisionSink interface {
	RecordPlanRevised(ctx context.Context, tx pgx.Tx, e PlanRevisedEvent) error
	RecordFeedback(ctx context.Context, e FeedbackEvent) (string, time.Time, error)
	BlockedSince(ctx context.Context, requirementKey string, since time.Time) ([]BlockedFeedback, error)
}

// CatalogPort is the scan-catalog readiness view the 前置门 needs
// (implemented by the composition root over the scan domain).
type CatalogPort interface {
	// Ready reports whether the repository has a completed scan profile
	// (AutoCard: profiled_at non-empty).
	Ready(ctx context.Context, name string) (bool, error)
}

// EscalationService runs the ladder and the collection window. Window
// defaults to 30s (协议 §2 步骤 3); tests shrink it.
type EscalationService struct {
	Store  *PostgresStore
	Sink   DecisionSink
	Window time.Duration
	Now    func() time.Time
	// Catalog 是 apply 前置门（GateReady）的就绪判定来源；nil = 全部视为就绪。
	Catalog CatalogPort
	// OnboardMissing 对缺失（未注册/未扫描）的仓库触发注册+单仓扫描；
	// 由组合根接线到 scan 域。nil = 不触发。协议 §3 触发特例。
	//
	// 2026-09-20：签名带 planID。扫描必须盖**组织章**
	// （repomesh_scan.repositories.organization_id），而扫描目录的读面按组织裁剪
	// —— 接线处要 planID → project → organization 才解析得出组织。不带 planID 就只能
	// 空 org 去扫：新仓库会被注册，但**在按组织裁剪的目录里看不见**，onboarding 于是
	// 变成一次"报成功但没用"的操作。
	OnboardMissing func(ctx context.Context, planID, repository string)
	// Adjacency 是第 2 期判定步骤的依赖视图（组合根接线到 scan 证据图：
	// BuildGraph + 别名解析）。nil = 判定按"无邻接"处理（仅 X 本身）。
	Adjacency AdjacencyPort
	// InterruptWait 是人工打断等待 X 就绪的上限（第 2 期判定前置；
	// 人工路径必须等待——用户就是为 X 打断的）。默认 5 分钟，测试缩短。
	InterruptWait time.Duration
}

// AdjacencyPort 是判定步骤的依赖邻接视图（组合根接线到 scan 证据图）。
type AdjacencyPort interface {
	// Neighbors 返回 X 的依赖（X 需要）与被依赖（需要 X）仓库名。
	Neighbors(ctx context.Context, name string) (dependsOn, dependedBy []string, err error)
}

// InterruptOutcome 汇报一次执行中人工打断的处理结果。
type InterruptOutcome struct {
	NodeID string `json:"nodeId"`
	// Onboarded：X 是本次新 onboarding 的（已注册过则为 false）。
	Onboarded bool `json:"onboarded"`
	// Ready：打断返回时 X 的 AutoCard 是否就绪（人工路径必须等待就绪）。
	Ready bool `json:"ready"`
	// AffectsPlan：判定 X 与当前计划 v1 是否有改动——true = 需要重排 v2。
	AffectsPlan bool `json:"affectsPlan"`
	// AffectedSet：有改动时的受影响集合（X + 依赖邻接 ∪ 计划既有仓库）。
	AffectedSet []string `json:"affectedSet,omitempty"`
	// ReplanQueued：本次打断**已经登记**了一次重排 v2 的派发意图（发现链第 6 步）。
	// false 有两种情形，界面要分开说：判定为"不影响计划"（本来就不需要重排），
	// 或重排端口未接线（该重排但没人产 v2 —— 这是**故障**，不是"无事发生"）。
	ReplanQueued bool `json:"replanQueued"`
}

// HumanInterrupt is one mid-execution manual repo addition (③).
type HumanInterrupt struct {
	PlanID         string
	UserID         string
	RepoName       string // X（仓库名，与 affectedRepositories 同一标识空间）
	Note           string
	IdempotencyKey string
}

// ReportBlocked records one ladder hop and returns the decision node id
// plus its creation time — the id is the escalation's upstream handle and
// the time anchors the collection window (触发报告 = 窗口起点).
func (s *EscalationService) ReportBlocked(ctx context.Context, r BlockedReport) (string, time.Time, error) {
	plan, err := s.Store.GetPlan(ctx, r.PlanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrUnknownPlan
	}
	if err != nil {
		return "", time.Time{}, err
	}
	key := r.IdempotencyKey
	if key == "" {
		key = newUUIDv4()
	}
	node, at, err := s.Sink.RecordFeedback(ctx, FeedbackEvent{
		RequirementText:      plan.RequirementText,
		RequirementKey:       plan.RequirementKey,
		ReporterID:           r.ReporterID,
		Role:                 r.Role,
		Reason:               r.Reason,
		AffectedRepositories: r.AffectedRepositories,
		UpstreamRef:          r.UpstreamRef,
		Status:               "blocked",
		IdempotencyKey:       key,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return node, at, nil
}

// BlockedReport is one escalation hop.
type BlockedReport struct {
	PlanID               string
	ReporterID           string
	Role                 FeedbackRole
	Reason               string
	AffectedRepositories []string
	UpstreamRef          string // 升级 hop 指向上一跳节点
	IdempotencyKey       string
}

// ResolveWithinRepo records the TM digest: the blocked report was solved
// inside the repository, ladder closed (append-only — the original node is
// never rewritten). IdempotencyKey 由调用方传入——重放返回同一节点，重试不会
// 重复落 closed 节点（交付规则：外部副作用需幂等键）。
func (s *EscalationService) ResolveWithinRepo(ctx context.Context, planID, blockedNodeID, byTM, note, idempotencyKey string) error {
	plan, err := s.Store.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if _, _, err := s.Sink.RecordFeedback(ctx, FeedbackEvent{
		RequirementText: plan.RequirementText,
		RequirementKey:  plan.RequirementKey,
		ReporterID:      byTM,
		Role:            RoleTM,
		Reason:          note,
		UpstreamRef:     blockedNodeID,
		Status:          "closed",
		IdempotencyKey:  idempotencyKey,
	}); err != nil {
		return err
	}
	return nil
}

// MarkDeprecatedAndCollect opens the collection window anchored at since
// (the triggering report's time): plan → deprecated (在跑任务继续), wait
// Window merging concurrent feedback, then collect the ladder leaves —
// blocked nodes whose id no child escalation references.
func (s *EscalationService) MarkDeprecatedAndCollect(ctx context.Context, planID string, since time.Time) ([]BlockedFeedback, error) {
	if err := s.Store.MarkDeprecated(ctx, planID); err != nil {
		return nil, err
	}
	plan, err := s.Store.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}

	// 窗口开启即触发已知缺失仓库的 onboarding（协议 §3：与收集窗并行、互不
	// 等待；审查 #2：等窗关才触发会损失 30 秒卡片就绪提前量）。
	known, err := s.Sink.BlockedSince(ctx, plan.RequirementKey, since)
	if err != nil {
		return nil, err
	}
	s.triggerOnboarding(ctx, planID, affectedRepos(known))

	window := s.Window
	if window <= 0 {
		window = 30 * time.Second
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}

	all, err := s.Sink.BlockedSince(ctx, plan.RequirementKey, since)
	if err != nil {
		return nil, err
	}
	leaves := ladderLeaves(all)

	// 窗口内新到达的反馈可能引入新的缺失仓库：幂等补触发（已就绪者跳过，
	// goroutine 自带 30 分钟上限；触发失败不阻塞收集，fail-open 与协议一致）。
	s.triggerOnboarding(ctx, planID, affectedRepos(leaves))

	return leaves, nil
}

// affectedRepos 去重收集反馈清单涉及的仓库名。
func affectedRepos(feedback []BlockedFeedback) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(feedback))
	for _, f := range feedback {
		for _, name := range f.AffectedRepositories {
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// triggerOnboarding 对缺失（未就绪）的仓库并行触发 onboarding（goroutine 自带
// 30 分钟上限；WithoutCancel 让 onboarding 存活于窗口 ctx 之外——协议：不互等）。
// 触发失败不阻塞收集（fail-open，与协议一致）。
func (s *EscalationService) triggerOnboarding(ctx context.Context, planID string, repos []string) {
	if s.OnboardMissing == nil {
		return
	}
	for _, name := range repos {
		name := name
		if s.Catalog != nil {
			if ready, err := s.Catalog.Ready(ctx, name); err == nil && ready {
				continue
			}
		}
		go func(name string) {
			ctx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx), 30*time.Minute)
			defer cancel()
			s.OnboardMissing(ctx, planID, name)
		}(name)
	}
}

// GateReady 是 apply 的前置门（协议 §2 步骤 4 前置门）：受影响仓库逐一校验
// AutoCard 就绪；未就绪者不进本轮 v2，留下一轮（调用方以返回值过滤批次）。
// Catalog 未配置时全部视为就绪。
func (s *EscalationService) GateReady(ctx context.Context, repos []string) (ready, pending []string, err error) {
	for _, name := range repos {
		if s.Catalog != nil {
			ok, err := s.Catalog.Ready(ctx, name)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				pending = append(pending, name)
				continue
			}
		}
		ready = append(ready, name)
	}
	return ready, pending, nil
}

// DropRepos 从批次快照中移除给定仓库（GateReady 的 v2 过滤形状；空批次随删）。
func DropRepos(batches [][]string, drop map[string]bool) (filtered [][]string) {
	for _, batch := range batches {
		var kept []string
		for _, repo := range batch {
			if !drop[repo] {
				kept = append(kept, repo)
			}
		}
		if len(kept) > 0 {
			filtered = append(filtered, kept)
		}
	}
	return filtered
}

// InterruptOutcome 语义（第 2 期）：
//   - Ready=false（等待超时）→ 暂定：X 未就绪，等就绪后用户可再次打断判定；
//   - Ready=true + AffectsPlan=true → 需要重排 v2（收集窗开启、受影响集合返回，
//     Leader/LLM 在其上产出 v2；apply 落 adjusted 节点）；
//   - Ready=true + AffectsPlan=false → 暂定：X 与当前计划无耦合，纯备用。

// InterruptPlanRepo 是 ③执行中人工打断（api-design 动态加仓库设计 §3.1）：
// 人类提交仓库 X → 落打断决策单（actor=human）→ onboarding（未就绪时）→
// 等待就绪 → 机械判定 X 是否影响当前计划 v1 → 有改动则开启收集窗返回受影响
// 集合（Leader/LLM 在其上重排 v2），无改动则暂定。幂等：同键重放同结果。
func (s *EscalationService) InterruptPlanRepo(ctx context.Context, cmd HumanInterrupt) (InterruptOutcome, error) {
	plan, err := s.Store.GetPlan(ctx, cmd.PlanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return InterruptOutcome{}, ErrUnknownPlan
	}
	if err != nil {
		return InterruptOutcome{}, err
	}
	name := strings.TrimSpace(cmd.RepoName)
	if name == "" {
		return InterruptOutcome{}, ErrEmptyRepo
	}

	out := InterruptOutcome{AffectedSet: []string{name}}
	// 1) 人工打断决策单（actor=human；幂等：同键重放返回同一节点）。
	node, _, err := s.ReportBlocked(ctx, BlockedReport{
		PlanID:               cmd.PlanID,
		ReporterID:           cmd.UserID,
		Role:                 RoleHuman,
		Reason:               cmd.Note,
		AffectedRepositories: []string{name},
		IdempotencyKey:       cmd.IdempotencyKey,
	})
	if err != nil {
		return out, err
	}
	out.NodeID = node

	// 2) onboarding：未就绪则触发注册+扫描，并等待就绪（人工路径必须等待）。
	ready := false
	if s.Catalog != nil {
		ready, err = s.Catalog.Ready(ctx, name)
		if err != nil {
			return out, err
		}
	}
	if !ready && s.OnboardMissing != nil {
		out.Onboarded = true
		s.OnboardMissing(ctx, cmd.PlanID, name)
		ready = s.waitReady(ctx, name)
	}
	out.Ready = ready
	if !ready {
		// 等待超时：暂定。决策单已记录，等就绪后用户可再次打断判定。
		return out, nil
	}

	// 3) 判定步骤（机械）：X 的依赖邻接与 v1 仓库集合求交。
	planRepos := map[string]bool{}
	for _, batch := range plan.Batches {
		for _, repo := range batch {
			planRepos[repo] = true
		}
	}
	var dependsOn, dependedBy []string
	if s.Adjacency != nil {
		dependsOn, dependedBy, err = s.Adjacency.Neighbors(ctx, name)
		if err != nil {
			return out, err
		}
	}
	affects := false
	affectedSet := map[string]bool{name: true}
	for _, group := range [][]string{dependsOn, dependedBy} {
		for _, repo := range group {
			affectedSet[repo] = true
			if planRepos[repo] {
				affects = true
			}
		}
	}
	out.AffectedSet = sortedSetKeys(affectedSet)
	if !affects {
		// 无改动 → 暂定：X 已入库备用，决策链已记录，不重排。
		return out, nil
	}

	// 4) 有改动：开启收集窗（合并并发打断/反馈），返回受影响集合供重排 v2。
	leaves, err := s.MarkDeprecatedAndCollect(ctx, cmd.PlanID, time.Now())
	if err != nil {
		return out, err
	}
	for _, leaf := range leaves {
		for _, repo := range leaf.AffectedRepositories {
			affectedSet[repo] = true
		}
	}
	out.AffectsPlan = true
	out.AffectedSet = sortedSetKeys(affectedSet)
	return out, nil
}

// waitReady 轮询就绪（人工路径必须等待；上限 InterruptWait，默认 5 分钟）。
func (s *EscalationService) waitReady(ctx context.Context, name string) bool {
	if s.Catalog == nil {
		return true // 与 GateReady 的 nil 语义一致（审查 #1）
	}
	wait := s.InterruptWait
	if wait <= 0 {
		wait = 5 * time.Minute
	}
	deadline := time.Now().Add(wait)
	for {
		ok, err := s.Catalog.Ready(ctx, name)
		if err == nil && ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func sortedSetKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ladderLeaves drops every node that another collected node escalates
// (parents stay in the chain for audit; the leaves are what the Leader
// replans on).
func ladderLeaves(all []BlockedFeedback) []BlockedFeedback {
	parents := map[string]bool{}
	for _, f := range all {
		if f.UpstreamRef != "" {
			parents[f.UpstreamRef] = true
		}
	}
	leaves := make([]BlockedFeedback, 0, len(all))
	for _, f := range all {
		if !parents[f.NodeID] {
			leaves = append(leaves, f)
		}
	}
	return leaves
}
