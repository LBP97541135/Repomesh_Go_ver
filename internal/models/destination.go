package models

import (
	"context"

	"repomesh.local/repomesh/internal/jsoninput"
)

// ReadSnapshotTarget parses the {providerId, providerRevision, modelRowId}
// body shared by the test preview and application preview endpoints.
func ReadSnapshotTarget(data []byte) (SnapshotTarget, error) {
	var wire struct {
		ProviderID       string `json:"providerId"`
		ProviderRevision string `json:"providerRevision"`
		ModelRowID       string `json:"modelRowId"`
	}
	if err := jsoninput.Decode(data, &wire, 256*1024); err != nil {
		return SnapshotTarget{}, validation("body")
	}
	if !validUUID(wire.ProviderID) {
		return SnapshotTarget{}, validation("providerId")
	}
	if wire.ProviderRevision == "" || len(wire.ProviderRevision) > 128 {
		return SnapshotTarget{}, validation("providerRevision")
	}
	if !validUUID(wire.ModelRowID) {
		return SnapshotTarget{}, validation("modelRowId")
	}
	return SnapshotTarget{ProviderID: wire.ProviderID, ProviderRevision: wire.ProviderRevision, ModelRowID: wire.ModelRowID}, nil
}

// ResolveDestination maps a completed test to its browser recovery page. The
// actor must own the test row; anything else is 404.
func (s *TestService) ResolveDestination(ctx context.Context, actor, testID string) (*string, error) {
	testID = normalizeKey(testID)
	if !validUUID(testID) {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_models.tests WHERE actor=$1 AND test_id=$2)`, actor, testID).Scan(&exists); err != nil {
		return nil, unavailable()
	}
	if !exists {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	path := "/settings/model-tests/" + testID
	return &path, nil
}

// ResolveDestination maps a completed application to its project-scoped
// browser recovery page. The actor must own the operation row.
func (s *ApplicationService) ResolveDestination(ctx context.Context, actor, projectID, applicationID string) (*string, error) {
	applicationID = normalizeKey(applicationID)
	if !validUUID(projectID) || !validUUID(applicationID) {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_models.application_operations WHERE project_id=$1 AND actor=$2 AND application_id=$3)`, projectID, actor, applicationID).Scan(&exists); err != nil {
		return nil, unavailable()
	}
	if !exists {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	path := "/projects/" + projectID + "/model-applications/" + applicationID
	return &path, nil
}
