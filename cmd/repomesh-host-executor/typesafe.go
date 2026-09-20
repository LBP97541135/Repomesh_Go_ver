package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"repomesh.local/repomesh/internal/typesafe"
)

// Helper commands run in an Agent process and deliberately do not open the
// execution database or load platform secrets.
func runTypeSafeHelper(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: typesafe install | cleanup | evaluate [request.json]")
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 1
	}
	switch args[0] {
	case "install":
		if os.Getenv("REPOMESH_TYPESAFE_GRANT") == "" {
			fmt.Fprintln(stderr, "TYPESAFE_TOOL_UNAVAILABLE")
			return 1
		}
		err = typesafe.InstallSkill(cwd, os.Getenv("REPOMESH_TYPESAFE_RUN_ID"))
	case "cleanup":
		err = typesafe.CleanupSkill(cwd, os.Getenv("REPOMESH_TYPESAFE_RUN_ID"))
	case "evaluate":
		var input io.Reader = os.Stdin
		if len(args) > 2 {
			return 2
		}
		if len(args) == 2 && args[1] != "-" {
			f, openErr := os.Open(args[1])
			if openErr != nil {
				fmt.Fprintln(stderr, "TYPESAFE_INPUT_UNREADABLE")
				return 1
			}
			defer f.Close()
			input = f
		}
		err = typesafe.Invoke(ctx, os.Getenv("REPOMESH_TYPESAFE_BROKER_URL"), os.Getenv("REPOMESH_TYPESAFE_GRANT"), input, stdout)
	default:
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

func (e *executor) typeSafeEnvironment(ctx context.Context, runID, workspace string) ([]string, func(), error) {
	g, err := typesafe.IssueGrant(ctx, e.pool, e.workerID, runID)
	if err != nil {
		return nil, func() {}, err
	}
	if g == nil {
		return nil, func() {}, nil
	}
	reason := ""
	finish := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := typesafe.CleanupSkill(filepath.Join(workspace, "repo"), runID); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "typesafe: skill cleanup incomplete")
		}
		if err := typesafe.FinishGrant(cleanupCtx, e.pool, runID, reason); err != nil {
			fmt.Fprintln(os.Stderr, "typesafe: grant finalization unavailable")
		}
	}
	broker := os.Getenv("REPOMESH_TYPESAFE_BROKER_URL")
	if _, err := typesafe.BrokerURL(broker); err != nil {
		reason = "TYPESAFE_BROKER_NOT_CONFIGURED"
		return nil, finish, nil
	}
	helper, err := os.Executable()
	if err != nil {
		reason = "TYPESAFE_HELPER_UNAVAILABLE"
		return nil, finish, nil
	}
	return []string{"REPOMESH_TYPESAFE_GRANT=" + g.Token, "REPOMESH_TYPESAFE_RUN_ID=" + runID,
		"REPOMESH_TYPESAFE_BROKER_URL=" + broker, "REPOMESH_TYPESAFE_HELPER=" + helper}, finish, nil
}
