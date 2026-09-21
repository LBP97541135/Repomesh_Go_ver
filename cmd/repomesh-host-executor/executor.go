package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	// sem 是并发闸：容量 = 同时在跑的 agent 数（REPOMESH_EXECUTOR_CONCURRENCY，
	// 默认 1 = 行为与改动前一字不变）。每认领一条 run 占一个槽，agent 退出时释放。
	//
	// 为什么是信号量而不是"起 N 个 goroutine 各跑一个循环"：认领查询靠
	// FOR UPDATE + LIMIT 1 天然互斥，多个循环也能工作，但**心跳与 stop 巡检**
	// 会被重复执行 N 次。用信号量把"认领"留在**一个**循环里，语义最接近原来那条。
	sem chan struct{}
}

// executorConcurrency 读并发度。非法值一律退回 1（最保守）—— 宁可慢，
// 也不要因为一个写错的环境变量把机器打爆。
func executorConcurrency() int {
	raw := strings.TrimSpace(os.Getenv("REPOMESH_EXECUTOR_CONCURRENCY"))
	if raw == "" {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 16 {
		return 1
	}
	return value
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
	concurrency := executorConcurrency()
	fmt.Fprintf(os.Stderr, "host-executor: 并发度 %d（REPOMESH_EXECUTOR_CONCURRENCY）\n", concurrency)
	return &executor{
		pool: pool, workerID: workerID, execution: execution.New(pool),
		sem: make(chan struct{}, concurrency),
	}, nil
}

func (e *executor) Close() { e.pool.Close() }

// ReconcileLostRuns 收尾"进程已经不在、台账还停在 running"的 run。
//
// 为什么必须在启动时跑一次：本 executor 监督子进程靠的是**本进程里等 `cmd.Wait()`**，
// 一旦自己被重启（部署 / systemd restart / OOM），那个等待就随进程消失，
// 而库里那条 run 会永远停在 running —— 任务也就永远交不回经理门（线上实测：
// saleor-app-template 那条开发 run 挂了 2 小时，任务一直"执行不完"）。
// 启动时先扫一遍，等于把上一次生命周期里丢掉的结局补记上。
func (e *executor) ReconcileLostRuns(ctx context.Context) {
	closed, err := e.execution.ReconcileLostRuns(ctx, e.workerID, processAlive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "host-executor: 孤儿 run 收尾失败: %v\n", err)
		return
	}
	if closed > 0 {
		fmt.Fprintf(os.Stderr, "host-executor: 已按「结局未知」收尾 %d 条孤儿 run\n", closed)
	}
}

// RunOne performs at most one bounded step: heartbeat, then launch one
// pending agent run, then handle any stop_requested attempt by revoking
// writes, releasing resources and letting the coordinator's ConfirmStopped
// record the formal transition.
func (e *executor) RunOne(ctx context.Context) error {
	if _, err := e.pool.Exec(ctx, `UPDATE repomesh_execution.workers SET heartbeat_at=clock_timestamp() WHERE id=$1`, e.workerID); err != nil {
		return errors.New("heartbeat failed")
	}
	// 并发闸（2026-09-22 用户要求并发 4）。
	//
	// 此前 launchAgentRun **阻塞等 agent 跑完**，所以整台机器一次只跑一条 run ——
	// 8 个任务排队串行，每个还要 clone + npm install，总时长是任务数的线性叠加。
	//
	// 闸门放在**认领之前**：先认领再等槽，会把 run 空占着（它 state 仍是 pending，
	// 看板上像"排队"，实际已分给一个不干活的执行器）。
	// 槽满时不空转：正好把 stop 巡检跑掉（原来槽满时那条路径根本走不到）。
	select {
	case e.sem <- struct{}{}:
	default:
		return e.sweepStopped(ctx)
	}
	// Agent launch first: a pending run takes priority over cleanup polling.
	command, runID, err := e.execution.ClaimAgentLaunch(ctx, e.workerID)
	if err != nil {
		<-e.sem
		return err
	}
	if runID != "" {
		parts, err := splitCommand(command.Command)
		if err != nil {
			<-e.sem
			_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
			return err
		}
		if err := e.prepareWorkspace(command.Workspace); err != nil {
			<-e.sem
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
				<-e.sem
				_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
				return fmt.Errorf("installation token for %s: %w", command.RepoFullName, mintErr)
			}
			ghToken = token
		}
		// 真正跑起来：放进 goroutine，主循环立刻回去认领下一条 —— 这就是并发的来源。
		//
		// 用 WithoutCancel：主循环的 ctx 在关停时会被取消，但那**不该**把已经跑起来的
		// agent 一起杀掉 —— 半途被杀会留下"结局未知"的 run，收尾交给
		// ReconcileLostRuns 与协调器的孤儿巡检。进程本身由 Setpgid 独立成组。
		go func() {
			defer func() { <-e.sem }()
			if err := e.launchAgentRun(context.WithoutCancel(ctx), parts, command.Workspace, runID, ghToken); err != nil {
				fmt.Fprintln(os.Stderr, "host-executor: agent run failed:", err)
			}
		}()
		return nil
	}
	<-e.sem
	return e.sweepStopped(ctx)
}

// sweepStopped 是原来 RunOne 里"没有 run 要启动"的那一半：巡检 stop_requested
// 的 attempt 并拆掉它占的资源。拆出来是因为并发之后它有了第二个调用点（槽满时）。
func (e *executor) sweepStopped(ctx context.Context) error {
	var attemptID string
	err := e.pool.QueryRow(ctx, `SELECT a.id FROM repomesh_execution.attempts a
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
