package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	skill "repomesh.local/repomesh/internal/skills"
	"repomesh.local/repomesh/internal/typesafe"
)

// A repository manager's advisory review is a separate run, never a test
// worker approving its own result. It cannot advance the manager gate.
func enqueueTypeSafeReview(ctx context.Context, tx pgx.Tx, projectID, issueID, revision, workerID, taskID, sourceAttempt, agentKind, model, requirement string) error {
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_typesafe.settings WHERE project_id=$1 AND enabled AND secret_version IS NOT NULL)`, projectID).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	attemptID, err := newRunID("att_review_")
	if err != nil {
		return err
	}
	runID, err := newRunID("run_review_")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id,project_id,issue_id,worker_id,configuration_revision,state,reservation_generation,launch_verified_at)
		VALUES($1,$2,$3,$4,$5,'launch_verified',
		COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3),1),clock_timestamp())`, attemptID, projectID, issueID, workerID, revision)
	if err != nil {
		return err
	}
	command, err := buildReviewCommand(agentKind, model, requirement, "/opt/repomesh/workspaces/"+sourceAttempt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs(id,attempt_id,agent_kind,command,workspace,task_package_ref,state)
		VALUES($1,$2,'review_agent',$3,$4,$5,'pending')`, runID, attemptID, command, "/opt/repomesh/workspaces/"+attemptID, taskID)
	return err
}

func buildReviewCommand(agentKind, model, requirement, sourceWorkspace string) (string, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9._:/-]{1,128}$`).MatchString(model) {
		return "", fmt.Errorf("invalid review model")
	}
	prompt := `You are assisting the repository manager (Repository Leader) with code review before the manager acceptance gate.
Review the candidate commit read-only against this approved requirement: ` + requirement + `.
Read git show HEAD, its changed-file list and relevant surrounding source. This is a private review checkout at the candidate SHA; do not modify business code, create commits, push, approve the task or merge. The source repository is not yours to edit.
Use the code-review skill below. Identify concrete behavior and findings with file/line evidence. Use Jev to check the narrow claims against the actual diff and context; include both potentially contradicted claims and supported claims where evidence exists. Do not ask Jev to invent a review or make a release decision. Missing context means insufficient. Include the actual full candidate SHA in the tool request. Produce findings and a suggested ACCEPT/RETRY/REASSIGN/ESCALATE decision for the repository manager; this is advice only.
` + skill.SeedDoc("code-review") + "\n" + typesafe.RuntimePrompt
	encodedPrompt := base64.StdEncoding.EncodeToString([]byte(prompt))
	var agent string
	switch agentKind {
	case "codex_cli":
		agent = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"$(cat \"$PROMPT\")\"", model)
	case "claude_cli":
		agent = fmt.Sprintf("claude -p \"$(cat \"$PROMPT\")\" --model %s --dangerously-skip-permissions", model)
	default:
		return "", fmt.Errorf("unsupported review agent kind %q", agentKind)
	}
	if strings.ContainsAny(sourceWorkspace, "'\"`$\\\n\r") {
		return "", fmt.Errorf("invalid review source")
	}
	script := "set -e\n" +
		"PROMPT=\"$PWD/review-prompt.txt\"\n" +
		"printf %s " + encodedPrompt + " | base64 -d > \"$PROMPT\"\n" +
		"SOURCE=\"" + sourceWorkspace + "/repo\"\n" +
		"COMMIT=$(git -C \"$SOURCE\" rev-parse HEAD)\n" +
		"git clone --no-hardlinks --no-checkout \"$SOURCE\" \"$PWD/repo\"\n" +
		"cd \"$PWD/repo\"\n" +
		"git remote remove origin\n" +
		"git checkout --detach \"$COMMIT\"\n" +
		typesafe.PrepareScript + agent + "\n"
	return "bash -c '" + script + "'", nil
}
