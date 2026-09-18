package delivery

import "context"

// Pusher is the port for the platform-side git operations on the workspace.
// The implementation shells out to git in the attempt workspace with the
// installation token injected only into the process environment of that one
// command; the token never lands in the workspace or any log.
type Pusher interface {
	// PushBranch pushes the workspace's current branch as name to origin and
	// returns the remote branch ref.
	PushBranch(ctx context.Context, workspace, branch string) (string, error)
}

// PullRequestOpener creates the pull request through the GitHub App
// installation already configured in the platform (B02 github client).
type PullRequestOpener interface {
	// OpenPullRequest opens a PR from branch into base and returns its URL.
	OpenPullRequest(ctx context.Context, token, owner, repo, branch, base, title, body string) (string, error)
}
