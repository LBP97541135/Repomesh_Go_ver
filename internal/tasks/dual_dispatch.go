// dual_dispatch.go implements M5's dispatch shape: one task yields one
// development run and one test run. The test run references the development
// run's workspace (diff/test evidence lives there) and its result gates the
// manager approval (Py: database_test_team plan/approval/evidence triad).
package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// DualDispatchResult reports both run rows created for one task.
type DualDispatchResult struct {
	DevelopmentAgentRunID string `json:"developmentAgentRunId"`
	TestAgentRunID        string `json:"testAgentRunId"`
	AttemptID             string `json:"attemptId"`
}

// DispatchDual creates the ledger reservation once and registers two pending
// agent runs on the same attempt workspace: the developer agent and the test
// agent. The test agent only observes (tests + diff); it never edits.
func (p *PostgresStore) DispatchDual(ctx context.Context, execution ExecutionFacade, workerID, taskID, agentKind, title, instruction string) (DualDispatchResult, error) {
	devAttemptID, err := execution.ReserveForTask(ctx, workerID, taskID, agentKind, title, instruction)
	if err != nil {
		return DualDispatchResult{}, err
	}
	testAttemptID, err := execution.ReserveForTask(ctx, workerID, taskID, "test_agent", "verify: "+title, instruction)
	if err != nil {
		return DualDispatchResult{}, err
	}
	devRunID, err := newRunID("run_dev_")
	if err != nil {
		return DualDispatchResult{}, err
	}
	testRunID, err := newRunID("run_test_")
	if err != nil {
		return DualDispatchResult{}, err
	}
	// Agent run rows live in repomesh_execution.agent_runs; dual dispatch
	// inserts both variants with the same attempt. The executor claims them
	// in kind order: development first, test after it exits.
	if _, err := p.pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,$3,$4,$5,$6,'pending') ON CONFLICT DO NOTHING`,
		devRunID, devAttemptID, agentKind, "dev:"+title, workspaceRef(devAttemptID), taskID); err != nil {
		return DualDispatchResult{}, fmt.Errorf("tasks: dev run insert failed: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,'test_agent',$3,$4,$5,'pending')`,
		testRunID, testAttemptID, "test:"+title, workspaceRef(devAttemptID), taskID); err != nil {
		return DualDispatchResult{}, fmt.Errorf("tasks: test run insert failed: %w", err)
	}
	return DualDispatchResult{
		DevelopmentAgentRunID: devRunID,
		TestAgentRunID:        testRunID,
		AttemptID:             devAttemptID,
	}, nil
}

func workspaceRef(attemptID string) string {
	return "/var/lib/repomesh/workspaces/" + attemptID
}

func newRunID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("tasks: id generation failed: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}
