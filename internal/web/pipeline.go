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
}
