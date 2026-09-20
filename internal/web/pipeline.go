package web

import (
	"context"

	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/delivery"
	"repomesh.local/repomesh/internal/deliverymanifest"
	"repomesh.local/repomesh/internal/interfacedoc"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/tasks"
)

// Pipeline bundles the M1-M9 services that previously were e2e-only; the web
// layer routes below make every one of them reachable in production.
type Pipeline struct {
	Tasks           *tasks.PostgresStore
	Assembly        *assembly.Service
	InterfaceDoc    *interfacedoc.Service
	BranchValid     *branchvalidation.Service
	Observation     *observability.Service
	Delivery        *delivery.Service
	DeliveryToken   string
	AgentTeams      *agentteams.Client
	Extensions      PipelineExtensions
	JointValidation JointValidation
	SCMRoutes       PipelineSCM
	HandoffDocs     HandoffDocs
	// Escalation 是升级梯（执行中人工打断 / 动态引入新仓库）。
	// nil = 这条能力未接线：/plans/{id}/interrupt 如实回 501，
	// **不假装受理**（2026-09-20 之前它就是那样一个空壳）。
	Escalation *tasks.EscalationService
	// ReplanHook 在人工打断判定"影响当前计划"之后被调用：它登记一次**重排 v2**
	// 的规划派发意图（发现链第 6 步）。nil = 未接线：打断照常返回判定结果，
	// 但**不会有 v2 产生** —— 前端据 replanQueued=false 如实显示，不假装计划动了。
	ReplanHook func(ctx context.Context, planID, upstreamNodeID string, affected []string) error
	// DeliveryManifests 是跨仓交付的**一致版本清单**（评委建议②）。
	// nil = 未接线：那条路由如实回 503，不假装有清单。
	DeliveryManifests *deliverymanifest.Service
}
