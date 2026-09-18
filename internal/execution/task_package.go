package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TaskPackage is the complete instruction set an agent receives. It carries
// goal, acceptance criteria, repository scope and the pinned configuration
// reference — never SCM credentials, never other issues' content.
type TaskPackage struct {
	IssueID            string
	ProjectID          string
	Title              string
	Description        string
	AcceptanceCriteria []string
	RepositoryIDs      []string
	ConfigurationRev   string
}

// WriteTaskPackage renders spec.md into the workspace and returns its path.
// The agent is trusted with the document, not with the platform: no tokens,
// no database URL, no other issue's body ever enter this file.
func WriteTaskPackage(ctx context.Context, workspace string, pkg TaskPackage) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", failure(422, "VALIDATION_FAILED")
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".repomesh"), 0o755); err != nil {
		return "", fmt.Errorf("workspace prepare failed: %w", err)
	}
	var body strings.Builder
	body.WriteString("# Task\n\n")
	body.WriteString("- issue: " + pkg.IssueID + "\n")
	body.WriteString("- project: " + pkg.ProjectID + "\n")
	body.WriteString("- configuration: " + pkg.ConfigurationRev + "\n\n")
	body.WriteString("## Goal\n\n" + pkg.Title + "\n\n")
	body.WriteString("## Description\n\n" + pkg.Description + "\n\n")
	if len(pkg.RepositoryIDs) > 0 {
		body.WriteString("## Scope\n\n")
		for _, id := range pkg.RepositoryIDs {
			body.WriteString("- " + id + "\n")
		}
		body.WriteString("\n")
	}
	if len(pkg.AcceptanceCriteria) > 0 {
		body.WriteString("## Acceptance\n\n")
		for index, criterion := range pkg.AcceptanceCriteria {
			body.WriteString(fmt.Sprintf("%d. %s\n", index+1, criterion))
		}
		body.WriteString("\n")
	}
	body.WriteString("## Boundaries\n\n")
	body.WriteString("- Commit your work in this workspace; do not push. The platform delivers.\n")
	body.WriteString("- Do not request or expect credentials; none are provisioned.\n")
	target := filepath.Join(workspace, ".repomesh", "spec.md")
	if err := os.WriteFile(target, []byte(body.String()), 0o644); err != nil {
		return "", fmt.Errorf("task package write failed: %w", err)
	}
	return target, nil
}
