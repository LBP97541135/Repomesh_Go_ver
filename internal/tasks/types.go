// Package tasks is the replan protocol's task axis (异常重规划协议 §2,
// Go 版): task entities, the assignment state machine, full-snapshot plan
// revisions, the escalation ladder and the collection window — built on the
// 47-table base (0009 platform core + 0010 task line) with the 0015
// protocol increment.
//
// Concurrency contract: every mutating method runs in one transaction with
// FOR UPDATE row locks and optimistic version/generation checks; any drift
// surfaces as ErrConflict and the caller retries against fresh state.
package tasks

import (
	"context"
	"errors"
	"time"
)

// ErrConflict reports a lost race: the plan version or attempt generation
// moved underneath the caller. Retry with fresh state.
var ErrConflict = errors.New("tasks: assignment state changed concurrently")

// TaskStatus follows the 0010 base vocabulary ('pending' default); the
// protocol adds assigned/blocked/superseded/done as the task moves.
type Status string

const (
	StatusPending    Status = "pending"
	StatusAssigned   Status = "assigned"
	StatusRunning    Status = "running"
	StatusBlocked    Status = "blocked"
	StatusSuperseded Status = "superseded"
	StatusDone       Status = "done"
)

// AttemptState follows the 0010 base vocabulary (default 'dispatched',
// B09/B10 will finalize the full set); the protocol requires completed and
// superseded as the migration terminals.
type AttemptState string

const (
	AttemptDispatched AttemptState = "dispatched"
	AttemptCompleted  AttemptState = "completed"
	AttemptSuperseded AttemptState = "superseded"
)

// Task is one unit of work bound to a repository, scoped to the 47-table
// tenancy (organization/project) and optionally to a plan. TaskUID is the
// stable identity across plan versions (显式 uid + 仓库名/标题兜底匹配).
type Task struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	ProjectID       string    `json:"projectId"`
	PlanID          string    `json:"planId,omitempty"`
	PlanVersion     string    `json:"planVersion,omitempty"`
	TaskUID         string    `json:"taskUid,omitempty"`
	RepositoryID    string    `json:"repositoryId,omitempty"`
	Title           string    `json:"title"`
	Instruction     string    `json:"instruction,omitempty"`
	Acceptance      string    `json:"acceptance,omitempty"`
	Status          Status    `json:"status"`
	AssigneeAgentID string    `json:"assigneeAgentId,omitempty"`
	Version         int       `json:"version"`
	IdempotencyKey  string    `json:"idempotencyKey,omitempty"`
	// 任务树读面的展示列(0029):批次号、协作房间会话 id 与执行者显示快照。
	BatchNo        *int      `json:"batchNo,omitempty"`
	ConversationID string    `json:"conversationId,omitempty"`
	LeaderLabel    string    `json:"leaderLabel,omitempty"`
	WorkerLabel    string    `json:"workerLabel,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Attempt is one assignment generation of a task (0010 task_assignments).
type Attempt struct {
	ID                string       `json:"id"`
	TaskID            string       `json:"taskId"`
	ManagerAgentID    string       `json:"managerAgentId"`
	WorkerAgentID     string       `json:"workerAgentId"`
	Generation        int          `json:"generation"`
	State             AttemptState `json:"state"`
	Reason            string       `json:"reason"`
	PreviousAttemptID string       `json:"previousAttemptId,omitempty"`
	ExecutionID       string       `json:"executionId,omitempty"`
	FinishedAt        *time.Time   `json:"finishedAt,omitempty"`
}

// TaskWrite is the creation shape.
type TaskWrite struct {
	OrganizationID string
	ProjectID      string
	PlanID         string
	PlanVersion    string
	TaskUID        string
	RepositoryID   string
	Title          string
	Instruction    string
	Acceptance     string
	IdempotencyKey string // optional; dedupes creation
}

// TaskSnapshot is one task inside a revision snapshot (v2 的任务全集)。
type TaskSnapshot struct {
	TaskUID      string `json:"taskUid,omitempty"`
	RepositoryID string `json:"repositoryId,omitempty"`
	Title        string `json:"title"`
}

// Store persists tasks and their assignment attempts.
type Store interface {
	// CreateTask inserts one task, idempotent by IdempotencyKey.
	CreateTask(ctx context.Context, w TaskWrite) (Task, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	// ListPlanTasks returns the plan's live (non-superseded) tasks, locked
	// for revision inside the caller's transaction when tx is given.
	ListPlanTasks(ctx context.Context, planID string) ([]Task, error)

	// Assign creates the first attempt (generation 1, expectedGeneration 0)
	// or supersedes the current worker with a replacement (generation+1).
	// The replacement worker must differ from the current one.
	Assign(ctx context.Context, taskID, managerAgentID, workerAgentID string, expectedTaskVersion, expectedGeneration int, reason string) (Attempt, error)
	// ReopenSameAssignment returns a task to its current worker.
	ReopenSameAssignment(ctx context.Context, taskID string, expectedTaskVersion, expectedGeneration int) error
	// BindExecution links the active attempt to a concrete execution run.
	BindExecution(ctx context.Context, taskID string, expectedGeneration int, executionID string) error
	// AllowsProjection is the supersede gate: only the current-generation,
	// non-superseded attempt may project results.
	AllowsProjection(ctx context.Context, taskID, attemptID string, generation int) (bool, error)
	// CompleteCurrent marks the active attempt completed; a concurrent
	// supersede is a conflict, completing twice is a no-op.
	CompleteCurrent(ctx context.Context, taskID, attemptID string, generation int) error
	// ActiveAttempt returns the current active attempt; pgx.ErrNoRows when
	// the task has none.
	ActiveAttempt(ctx context.Context, taskID string) (*Attempt, error)
}
