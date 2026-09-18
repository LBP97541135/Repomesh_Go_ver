package delivery

import (
	"context"
	"fmt"
)

// Service orchestrates the human-approved release: push the attempt workspace
// branch, then open the pull request. Both steps are ports so the web layer
// can wire real implementations while tests fake them.
type Service struct {
	pusher Pusher
	opener PullRequestOpener
}

// New wires the delivery service.
func New(pusher Pusher, opener PullRequestOpener) *Service {
	return &Service{pusher: pusher, opener: opener}
}

// ReleaseCommand is one human-approved release of an attempt workspace.
type ReleaseCommand struct {
	Workspace   string
	Branch      string
	Base        string
	Token       string
	Owner       string
	Repo        string
	Title       string
	Body        string
}

// ReleaseResult reports the pushed ref and pull request URL.
type ReleaseResult struct {
	BranchRef string `json:"branchRef"`
	PullURL   string `json:"pullUrl"`
}

// Release performs push-then-PR. The push happens first; a lost response
// after a successful push is safe to retry (the PR step is idempotent via
// GitHub's existing-PR handling).
func (s *Service) Release(ctx context.Context, command ReleaseCommand) (ReleaseResult, error) {
	if command.Workspace == "" || command.Branch == "" || command.Token == "" || command.Owner == "" || command.Repo == "" {
		return ReleaseResult{}, fmt.Errorf("delivery: workspace, branch, token, owner and repo are required")
	}
	branchRef, err := s.pusher.PushBranch(ctx, command.Workspace, command.Branch)
	if err != nil {
		return ReleaseResult{}, err
	}
	pullURL, err := s.opener.OpenPullRequest(ctx, command.Token, command.Owner, command.Repo, command.Branch, command.Base, command.Title, command.Body)
	if err != nil {
		return ReleaseResult{BranchRef: branchRef}, err
	}
	return ReleaseResult{BranchRef: branchRef, PullURL: pullURL}, nil
}
