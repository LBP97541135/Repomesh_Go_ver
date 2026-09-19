//go:build !unix

package main

import (
	"context"
	"errors"
)

var errNotSupported = errors.New("agent supervision requires a unix host")

// launchAgentRun is only implemented on unix hosts; other platforms report
// the gap instead of pretending to supervise processes.
func (e *executor) launchAgentRun(ctx context.Context, command []string, workspace, runID, ghToken string) error {
	return errNotSupported
}
