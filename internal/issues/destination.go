package issues

import (
	"context"
)

// ResolveDestination maps an issue_create operation destination to the browser
// creation page path; nil means the destination is not ours and the access
// layer leaves it alone.
func (s *Service) ResolveDestination(ctx context.Context, actor, projectID, operationID string) (*string, error) {
	path := "/projects/" + projectID + "/issue-creations/" + operationID
	return &path, nil
}
