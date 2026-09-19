package web

import (
	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/delivery"
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
}
