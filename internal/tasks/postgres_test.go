package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/testdb"
)

func newTask(t *testing.T, store *PostgresStore, key string) Task {
	t.Helper()
	org, project := newUUIDv4(), newUUIDv4()
	task, err := store.CreateTask(context.Background(), TaskWrite{
		OrganizationID: org, ProjectID: project, PlanID: newUUIDv4(),
		RepositoryID: "repo-a", Title: "改造网关协议", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestCreateTaskIdempotent(t *testing.T) {
	store := NewPostgresStore(testdb.Open(t))
	ctx := context.Background()

	first := newTask(t, store, "evt-create-1")
	again, err := store.CreateTask(ctx, TaskWrite{
		OrganizationID: first.OrganizationID, ProjectID: first.ProjectID,
		PlanID: first.PlanID, RepositoryID: "repo-other",
		Title: "不同的请求体", IdempotencyKey: "evt-create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.Title != first.Title {
		t.Fatal("idempotent replay must return the original task unchanged")
	}
}

func TestAssignLifecycleAndProjectionGate(t *testing.T) {
	store := NewPostgresStore(testdb.Open(t))
	ctx := context.Background()
	task := newTask(t, store, "evt-life-1")

	workerA := newUUIDv4()
	attempt, err := store.Assign(ctx, task.ID, newUUIDv4(), workerA, task.Version, 0, "首派")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Generation != 1 || attempt.State != AttemptDispatched {
		t.Fatalf("first attempt = %+v, want dispatched gen 1", attempt)
	}
	got, err := store.GetTask(ctx, task.ID)
	if err != nil || got.Status != StatusAssigned || got.AssigneeAgentID != workerA || got.Version != task.Version+1 {
		t.Fatalf("task after assign = %+v err %v", got, err)
	}

	if err := store.BindExecution(ctx, task.ID, 1, newUUIDv4()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.AllowsProjection(ctx, task.ID, attempt.ID, 1); !ok {
		t.Fatal("active attempt must be allowed to project")
	}

	if err := store.CompleteCurrent(ctx, task.ID, attempt.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteCurrent(ctx, task.ID, attempt.ID, 1); err != nil {
		t.Fatalf("complete twice must be a no-op, got %v", err)
	}
}

func TestSupersedeAndProjectionGate(t *testing.T) {
	store := NewPostgresStore(testdb.Open(t))
	ctx := context.Background()
	task := newTask(t, store, "evt-sup-1")

	workerA := newUUIDv4()
	first, err := store.Assign(ctx, task.ID, newUUIDv4(), workerA, task.Version, 0, "首派")
	if err != nil {
		t.Fatal(err)
	}
	afterAssign, _ := store.GetTask(ctx, task.ID)

	workerB := newUUIDv4()
	second, err := store.Assign(ctx, task.ID, newUUIDv4(), workerB, afterAssign.Version, 1, "异常重规划换人")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 2 || second.PreviousAttemptID != first.ID {
		t.Fatalf("supersede attempt = %+v", second)
	}

	if ok, _ := store.AllowsProjection(ctx, task.ID, first.ID, 1); ok {
		t.Fatal("superseded attempt must not project")
	}
	if ok, _ := store.AllowsProjection(ctx, task.ID, second.ID, 2); !ok {
		t.Fatal("replacement attempt must project")
	}
	if err := store.CompleteCurrent(ctx, task.ID, first.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("complete on superseded = %v, want ErrConflict", err)
	}

	if _, err := store.Assign(ctx, task.ID, newUUIDv4(), workerB, afterAssign.Version, 2, "same"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-worker replacement = %v, want ErrConflict", err)
	}
	if _, err := store.Assign(ctx, task.ID, newUUIDv4(), "worker-c", afterAssign.Version-1, 2, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version = %v, want ErrConflict", err)
	}
	if _, err := store.Assign(ctx, task.ID, newUUIDv4(), "worker-c", afterAssign.Version+1, 9, "stale gen"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale generation = %v, want ErrConflict", err)
	}
}

func TestConcurrentAssignSingleWinner(t *testing.T) {
	store := NewPostgresStore(testdb.Open(t))
	ctx := context.Background()
	task := newTask(t, store, "evt-conc-1")

	const workers = 8
	var wg sync.WaitGroup
	winners := make(chan string, workers)
	losses := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			attempt, err := store.Assign(ctx, task.ID, newUUIDv4(), newUUIDv4(),
				task.Version, 0, "并发首派")
			if err != nil {
				losses <- err
				return
			}
			winners <- attempt.WorkerAgentID
		}(i)
	}
	wg.Wait()
	close(winners)
	close(losses)

	won := 0
	for range winners {
		won++
	}
	if won != 1 {
		t.Fatalf("winners = %d, want exactly 1", won)
	}
	for err := range losses {
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("loser error = %v, want ErrConflict", err)
		}
	}
	active, err := store.ActiveAttempt(ctx, task.ID)
	if err != nil || active.Generation != 1 {
		t.Fatalf("active attempt = %+v err %v, want gen 1", active, err)
	}
}

func TestReopenAndMissingTask(t *testing.T) {
	store := NewPostgresStore(testdb.Open(t))
	ctx := context.Background()
	task := newTask(t, store, "evt-reopen-1")

	if _, err := store.Assign(ctx, task.ID, newUUIDv4(), newUUIDv4(), task.Version, 0, "首派"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetTask(ctx, task.ID)
	if err := store.ReopenSameAssignment(ctx, task.ID, after.Version, 1); err != nil {
		t.Fatal(err)
	}
	reopened, _ := store.GetTask(ctx, task.ID)
	if reopened.Status != StatusAssigned || reopened.Version != after.Version+1 {
		t.Fatalf("reopened = %+v", reopened)
	}

	if err := store.ReopenSameAssignment(ctx, task.ID, 9999, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale reopen = %v, want ErrConflict", err)
	}
	if _, err := store.GetTask(ctx, newUUIDv4()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing task = %v, want ErrNoRows", err)
	}
}
