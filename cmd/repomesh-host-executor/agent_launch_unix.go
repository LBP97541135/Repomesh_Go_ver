//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// launchAgentRun supervises one agent process end to end: start inside the
// attempt workspace, record the pid, wait, record the exit. The agent command
// runs headless; stdout/stderr land in per-run files inside the workspace so
// a failed run leaves a readable cause instead of a bare exit code.
func (e *executor) launchAgentRun(ctx context.Context, command []string, workspace, runID, ghToken string) error {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = sanitizedEnv(ghToken)
	// C6 fix: capture agent output next to the run instead of /dev/null.
	if stdout, err := os.OpenFile(filepath.Join(workspace, "agent-stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer stdout.Close()
		cmd.Stdout = stdout
	}
	if stderr, err := os.OpenFile(filepath.Join(workspace, "agent-stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer stderr.Close()
		cmd.Stderr = stderr
	}
	if err := cmd.Start(); err != nil {
		_ = e.execution.MarkAgentLaunchFailed(ctx, runID)
		return errors.New("agent launch failed: " + err.Error())
	}
	pid := cmd.Process.Pid
	if err := e.execution.MarkAgentRunning(ctx, runID, int64(pid)); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Wait()
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		code := 0
		killed := false
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
				killed = killedBySignal(exitErr)
			} else {
				code = -1
			}
		}
		return e.execution.MarkAgentExited(ctx, runID, code, killed, "")
	case <-ctx.Done():
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		timer := time.AfterFunc(10*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		defer timer.Stop()
		err := <-exited
		code := 0
		killed := true
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else {
				code = -1
			}
		}
		return e.execution.MarkAgentExited(context.Background(), runID, code, killed, "")
	}
}

func killedBySignal(exitErr *exec.ExitError) bool {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}

// sanitizedEnv strips every credential-bearing variable the executor process
// holds except the agent-scoped MINIMAX_API_KEY: codex reads its model-provider
// key from that env var (model_providers.minimax.env_key), and it never equals
// a platform secret.
func sanitizedEnv(ghToken string) []string {
	keep := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TERM": true,
		"TMPDIR": true, "USER": true, "SHELL": true, "MINIMAX_API_KEY": true}
	var result []string
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && keep[name] {
			result = append(result, entry)
		}
	}
	// 2026-09-19 C 修复：本次运行的仓库级 installation token 只以环境变量形式
	// 交给子进程（交付脚本里的 $T），既不在磁盘留缓存文件，也不进命令台账。
	if ghToken != "" {
		result = append(result, "REPOMESH_GH_TOKEN="+ghToken)
	}
	return result
}
