package assembly

import "context"

// TeamProvisioner creates the remote AgentTeams resources for one repository
// team. It is a port so tests can fake it and so the AT-free mode degrades to
// local-only topology records (真实的 blocked，不伪造远端成功).
type TeamProvisioner interface {
	// ProvisionTeam creates one AT team + its manager/worker members and
	// returns the remote team room reference (empty when unavailable).
	ProvisionTeam(ctx context.Context, repositoryID, managerName string, workerNames []string) (roomRef string, err error)
}
