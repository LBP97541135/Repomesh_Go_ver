package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
)

// executor is the restricted host-side loop. It owns no business state: it
// heartbeat-registers its worker row, polls for stop requests on its own
// attempts, provisions resources on demand, revokes write capability, and
// releases resources after verified teardown. It never advances attempt
// states itself (that is the coordinator's formal write) and never runs
// arbitrary host commands.
type executor struct {
	pool      *pgxpool.Pool
	workerID  string
	execution *execution.Service
}

func openExecution(ctx context.Context, databaseURL, workerID string) (*executor, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid database URL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("database unavailable")
	}
	_, err = pool.Exec(ctx, `INSERT INTO repomesh_execution.workers (id, host, kind, heartbeat_at)
		VALUES ($1, $2, 'host_executor', clock_timestamp())
		ON CONFLICT (id) DO UPDATE SET heartbeat_at=clock_timestamp(), retired_at=NULL`,
		workerID, hostname())
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("worker registration failed: %w", err)
	}
	return &executor{pool: pool, workerID: workerID, execution: execution.New(pool)}, nil
}

func (e *executor) Close() { e.pool.Close() }

// RunOne performs at most one bounded step: heartbeat, then launch one
// pending agent run, then handle any stop_requested attempt by revoking
// writes, releasing resources and letting the coordinator's ConfirmStopped
// record the formal transition.
func (e *executor) RunOne(ctx context.Context) error {
	if _, err := e.pool.Exec(ctx, `UPDATE repomesh_execution.workers SET heartbeat_at=clock_timestamp() WHERE id=$1`, e.workerID); err != nil {
		return errors.New("heartbeat failed")
	}
	// Agent launch first: a pending run takes priority over cleanup polling.
	command, runID, err := e.execution.ClaimAgentLaunch(ctx, e.workerID)
	if err != nil {
		return err
	}
	if runID != "" {
		parts, err := splitCommand(command.Command)
		if err != nil {
			_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
			return err
		}
		if err := e.prepareWorkspace(command.Workspace); err != nil {
			_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
			return err
		}
		// C 修复（2026-09-19）：installation token **按仓库现场铸**，不再读部署级
		// 缓存文件 .gh-token（那个文件由 timer 写死单个仓库刷新，别人把 App 装到
		// 自己账号上也推不动）。令牌只进子进程环境变量：不落盘、不进命令台账。
		ghToken := ""
		if command.RepoFullName != "" {
			token, mintErr := mintInstallationToken(ctx, command.RepoFullName)
			if mintErr != nil {
				_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
				return fmt.Errorf("installation token for %s: %w", command.RepoFullName, mintErr)
			}
			ghToken = token
		}
		return e.launchAgentRun(ctx, parts, command.Workspace, runID, ghToken)
	}
	var attemptID string
	err = e.pool.QueryRow(ctx, `SELECT a.id FROM repomesh_execution.attempts a
		JOIN repomesh_execution.workers w ON w.id=a.worker_id
		WHERE w.id=$1 AND a.state='stop_requested' LIMIT 1`, e.workerID).Scan(&attemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errors.New("poll failed")
	}
	resources, err := e.provisionedResources(ctx, attemptID)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if err := e.teardown(ctx, resource); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "host-executor: cleaned attempt %s (%d resources)\n", attemptID, len(resources))
	return nil
}

type resourceRow struct {
	id    string
	kind  string
	ref   string
	write bool
}

func (e *executor) provisionedResources(ctx context.Context, attemptID string) ([]resourceRow, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, kind, external_ref, write_enabled FROM repomesh_execution.host_resources
		WHERE attempt_id=$1 AND state='provisioned'`, attemptID)
	if err != nil {
		return nil, errors.New("resource listing failed")
	}
	defer rows.Close()
	result := []resourceRow{}
	for rows.Next() {
		var row resourceRow
		if rows.Scan(&row.id, &row.kind, &row.ref, &row.write) != nil {
			return nil, errors.New("resource listing failed")
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// teardown revokes write capability first, then performs the host-side stop
// for the resource kind, then marks it released. A host teardown failure
// keeps the resource provisioned; the attempt never reaches stopped with
// live resources (SQL trigger enforces the same invariant).
func (e *executor) teardown(ctx context.Context, resource resourceRow) error {
	if _, err := e.pool.Exec(ctx, `UPDATE repomesh_execution.host_resources SET write_enabled=false WHERE id=$1`, resource.id); err != nil {
		return errors.New("write revocation failed")
	}
	if err := stopHostResource(resource); err != nil {
		return err
	}
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = ctxWithTimeout
	if _, err := e.pool.Exec(ctx, `UPDATE repomesh_execution.host_resources
		SET state='released', released_at=clock_timestamp() WHERE id=$1 AND write_enabled=false`, resource.id); err != nil {
		return errors.New("release failed")
	}
	return nil
}

// stopHostResource is the single place raw host authority is exercised. In
// this build only directory resources (workspace teardown) have a local
// implementation; container/volume/network teardown requires the container
// runtime grant and stays explicitly unsupported rather than best-effort.
func stopHostResource(resource resourceRow) error {
	switch resource.kind {
	case "directory":
		return nil
	default:
		return fmt.Errorf("host teardown for %s not granted in this build", resource.kind)
	}
}

// splitCommand splits a stored agent command into argv, POSIX-shell style:
// whitespace separates words, single and double quotes group words together,
// and unmatched quotes are a launch failure rather than a silent misparse.
// strings.Fields was quote-unaware and exploded any prompt containing
// spaces into dozens of argv entries (codex usage-error exit 2).
func splitCommand(command string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inWord := false
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				current.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				parts = append(parts, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("agent command has an unmatched quote")
	}
	if inWord {
		parts = append(parts, current.String())
	}
	return parts, nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown-host"
	}
	return name
}

// prepareWorkspace creates the attempt workspace directory if missing. The
// agent works only inside this boundary.
func (e *executor) prepareWorkspace(workspace string) error {
	if workspace == "" {
		return errors.New("empty workspace")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return errors.New("workspace prepare failed")
	}
	return nil
}
