package delivery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitPusher implements Pusher by shelling out to git inside the workspace.
// The token is injected only into this command's environment (as an http
// extraheader credential) and never written to disk or logs.
type GitPusher struct{}

// PushBranch pushes HEAD to origin as branch.
func (GitPusher) PushBranch(ctx context.Context, workspace, branch string) (string, error) {
	if workspace == "" || branch == "" {
		return "", fmt.Errorf("delivery: workspace and branch required")
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			trimmed := strings.TrimSpace(string(output))
			if len(trimmed) > 400 {
				trimmed = trimmed[:400]
			}
			return fmt.Errorf("delivery: git %s failed: %s", args[0], trimmed)
		}
		return nil
	}
	if err := run("push", "origin", fmt.Sprintf("HEAD:refs/heads/%s", branch)); err != nil {
		return "", err
	}
	return "refs/heads/" + branch, nil
}

var _ Pusher = GitPusher{}
