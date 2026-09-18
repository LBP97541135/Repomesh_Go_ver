package assembly

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AssemblyCommand requests the topology for one organization.
type AssemblyCommand struct {
	OrganizationID string
	Repositories   []string // repository ids from the scan
	WorkersPerRepo int
	LeaderName     string
}

// AssemblyResult reports the created role identities.
type AssemblyResult struct {
	LeaderAgentID string   `json:"leaderAgentId"`
	Managers      []string `json:"managers"`
	Workers       []string `json:"workers"`
	TeamRooms     []string `json:"teamRooms"`
}

// Assemble provisions the leader (org singleton), then one manager + N
// workers per repository. The leader-never-a-worker invariant is enforced by
// construction: leader identity is created once and never listed as worker.
func (s *Service) Assemble(ctx context.Context, command AssemblyCommand) (AssemblyResult, error) {
	if command.OrganizationID == "" || len(command.Repositories) == 0 || command.LeaderName == "" {
		return AssemblyResult{}, fmt.Errorf("assembly: organization, repositories and leader name are required")
	}
	if command.WorkersPerRepo < 1 {
		command.WorkersPerRepo = 1
	}
	if command.WorkersPerRepo > 8 {
		command.WorkersPerRepo = 8
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AssemblyResult{}, fmt.Errorf("assembly: database unavailable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	leaderID, err := s.ensureAgent(ctx, tx, command.OrganizationID, "leader", "", command.LeaderName)
	if err != nil {
		return AssemblyResult{}, err
	}
	result := AssemblyResult{LeaderAgentID: leaderID}
	for _, repositoryID := range command.Repositories {
		managerName := "mgr-" + shortName(repositoryID)
		managerID, err := s.ensureAgent(ctx, tx, command.OrganizationID, "manager", repositoryID, managerName)
		if err != nil {
			return AssemblyResult{}, err
		}
		result.Managers = append(result.Managers, managerID)
		var workerNames []string
		for workerIndex := 0; workerIndex < command.WorkersPerRepo; workerIndex++ {
			workerName := fmt.Sprintf("wrk-%s-%d", shortName(repositoryID), workerIndex)
			if _, err := s.ensureAgent(ctx, tx, command.OrganizationID, "worker", repositoryID, workerName); err != nil {
				return AssemblyResult{}, err
			}
			workerNames = append(workerNames, workerName)
		}
		if s.provisioner != nil {
			roomRef, err := s.provisioner.ProvisionTeam(ctx, repositoryID, managerName, workerNames)
			if err != nil {
				return AssemblyResult{}, fmt.Errorf("assembly: remote provisioning failed for %s: %w", repositoryID, err)
			}
			if roomRef != "" {
				result.TeamRooms = append(result.TeamRooms, roomRef)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AssemblyResult{}, fmt.Errorf("assembly: commit failed: %w", err)
	}
	return result, nil
}

// ensureAgent inserts one agent identity keyed by its org+role+repository
// singleton; re-running the assembly is idempotent (singleton_key unique).
func (s *Service) ensureAgent(ctx context.Context, tx pgxTx, organizationID, role, repositoryID, name string) (string, error) {
	singleton := organizationID + ":" + role + ":" + repositoryID + ":" + name
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM public.agents WHERE singleton_key=$1`, singleton).Scan(&id)
	if err == nil {
		return id, nil
	}
	agentID, err := newAgentUUID()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.agents (id, organization_id, role, repository_id, singleton_key, resource_ref)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,'{}'::jsonb)`,
		agentID, organizationID, role, repositoryID, singleton); err != nil {
		return "", fmt.Errorf("assembly: agent insert failed: %w", err)
	}
	return agentID, nil
}

func shortName(repositoryID string) string {
	if len(repositoryID) > 12 {
		return repositoryID[len(repositoryID)-12:]
	}
	return repositoryID
}

// pgxTx is the transaction interface subset used by ensureAgent.
type pgxTx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TopologyRow is one agent identity in the project topology view.
type TopologyRow struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	RepositoryID string `json:"repositoryId,omitempty"`
	SingletonKey string `json:"singletonKey"`
}

// ListTopology returns the agent identities recorded for one project scope.
func (s *Service) ListTopology(ctx context.Context, projectID string) ([]TopologyRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, role, COALESCE(repository_id,''), singleton_key
		FROM public.agents WHERE singleton_key LIKE '%:' || $1 || ':%' OR repository_id=$1
		ORDER BY role, singleton_key`, projectID)
	if err != nil {
		return nil, fmt.Errorf("assembly: topology query failed: %w", err)
	}
	defer rows.Close()
	result := []TopologyRow{}
	for rows.Next() {
		var row TopologyRow
		if rows.Scan(&row.ID, &row.Role, &row.RepositoryID, &row.SingletonKey) != nil {
			return nil, fmt.Errorf("assembly: topology scan failed")
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
