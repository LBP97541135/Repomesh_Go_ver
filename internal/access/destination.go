package access

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type Destination struct {
	canonical string
	home      bool
}

type ProjectDestination struct {
	Kind          string
	ProjectID     string
	OperationKind string
	OperationID   string
}

type ProjectDestinationResolver func(context.Context, string, ProjectDestination) (*string, error)
type ModelSaveDestinationResolver func(context.Context, string, string) (*string, error)
type ModelTestDestinationResolver func(context.Context, string, string) (*string, error)
type ModelApplyDestinationResolver func(context.Context, string, string, string) (*string, error)
type IssueCreationDestinationResolver func(context.Context, string, string, string) (*string, error)

func (s *Service) SetProjectDestinationResolver(resolver ProjectDestinationResolver) {
	s.projectDestinationResolver = resolver
}

func (s *Service) SetModelSaveDestinationResolver(resolver ModelSaveDestinationResolver) {
	s.modelSaveDestinationResolver = resolver
}

func (s *Service) SetModelTestDestinationResolver(resolver ModelTestDestinationResolver) {
	s.modelTestDestinationResolver = resolver
}

func (s *Service) SetModelApplyDestinationResolver(resolver ModelApplyDestinationResolver) {
	s.modelApplyDestinationResolver = resolver
}

func (s *Service) SetIssueCreationDestinationResolver(resolver IssueCreationDestinationResolver) {
	s.issueCreationDestinationResolver = resolver
}

func (d Destination) ModelSaveDestination() (string, bool) {
	if d.canonical == "" || d.home {
		return "", false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(d.canonical), &fields) != nil {
		return "", false
	}
	var kind, operationKind, operationID string
	_ = json.Unmarshal(fields["kind"], &kind)
	_ = json.Unmarshal(fields["operationKind"], &operationKind)
	_ = json.Unmarshal(fields["operationId"], &operationID)
	if kind != "operation" || operationKind != "provider_save" {
		return "", false
	}
	id, err := normalizeProviderSaveID(operationID)
	if err != nil {
		return "", false
	}
	return id, true
}

func (s *Service) resolveModelSaveDestination(ctx context.Context, actor string, destination Destination) (*string, error) {
	saveID, ok := destination.ModelSaveDestination()
	if !ok || s.modelSaveDestinationResolver == nil {
		return nil, nil
	}
	return s.modelSaveDestinationResolver(ctx, actor, saveID)
}

func (d Destination) ModelTestDestination() (string, bool) {
	fields, ok := d.operationDestination()
	if !ok {
		return "", false
	}
	var operationKind, operationID string
	_ = json.Unmarshal(fields["operationKind"], &operationKind)
	_ = json.Unmarshal(fields["operationId"], &operationID)
	if operationKind != "model_test" {
		return "", false
	}
	id, err := normalizeOperationID(operationID)
	if err != nil {
		return "", false
	}
	return id, true
}

func (d Destination) ModelApplyDestination() (projectID, applicationID string, ok bool) {
	fields, hasFields := d.operationDestination()
	if !hasFields {
		return "", "", false
	}
	var operationKind, operationID, boundProject string
	_ = json.Unmarshal(fields["operationKind"], &operationKind)
	_ = json.Unmarshal(fields["operationId"], &operationID)
	_ = json.Unmarshal(fields["projectId"], &boundProject)
	if operationKind != "model_apply" {
		return "", "", false
	}
	if err := validProjectID(boundProject); err != nil {
		return "", "", false
	}
	id, err := normalizeOperationID(operationID)
	if err != nil {
		return "", "", false
	}
	return boundProject, id, true
}

// operationDestination is the shared guard for operation destinations: not
// home, a canonical object, kind == "operation".
func (d Destination) operationDestination() (map[string]json.RawMessage, bool) {
	if d.canonical == "" || d.home {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(d.canonical), &fields) != nil || fields == nil {
		return nil, false
	}
	var kind string
	_ = json.Unmarshal(fields["kind"], &kind)
	if kind != "operation" {
		return nil, false
	}
	return fields, true
}

func validProjectID(raw string) error {
	if !ValidID(strings.ToLower(raw)) {
		return failure(422, "VALIDATION_FAILED")
	}
	return nil
}

func normalizeOperationID(raw string) (string, error) {
	value := strings.ToLower(raw)
	if !ValidID(value) {
		return "", failure(422, "VALIDATION_FAILED")
	}
	return value, nil
}

func normalizeProviderSaveID(raw string) (string, error) {
	value := strings.ToLower(raw)
	if !ValidID(value) {
		return "", failure(422, "VALIDATION_FAILED")
	}
	return value, nil
}

func (d Destination) ProjectDestination() (ProjectDestination, bool) {
	if d.canonical == "" || d.home {
		return ProjectDestination{}, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(d.canonical), &fields) != nil {
		return ProjectDestination{}, false
	}
	get := func(key string) string {
		var value string
		_ = json.Unmarshal(fields[key], &value)
		return value
	}
	result := ProjectDestination{Kind: get("kind"), ProjectID: get("projectId"), OperationKind: get("operationKind"), OperationID: get("operationId")}
	if result.Kind == "project" || result.Kind == "operation" && (result.OperationKind == "project_create" || result.OperationKind == "project_update") {
		return result, true
	}
	return ProjectDestination{}, false
}

func (s *Service) resolveProjectDestination(ctx context.Context, actor string, destination Destination) (*string, error) {
	target, ok := destination.ProjectDestination()
	if !ok || s.projectDestinationResolver == nil {
		return nil, nil
	}
	return s.projectDestinationResolver(ctx, actor, target)
}

func (s *Service) resolveModelTestDestination(ctx context.Context, actor string, destination Destination) (*string, error) {
	testID, ok := destination.ModelTestDestination()
	if !ok || s.modelTestDestinationResolver == nil {
		return nil, nil
	}
	return s.modelTestDestinationResolver(ctx, actor, testID)
}

func (s *Service) resolveModelApplyDestination(ctx context.Context, actor string, destination Destination) (*string, error) {
	projectID, applicationID, ok := destination.ModelApplyDestination()
	if !ok || s.modelApplyDestinationResolver == nil {
		return nil, nil
	}
	return s.modelApplyDestinationResolver(ctx, actor, projectID, applicationID)
}

func (d Destination) IssueCreationDestination() (projectID, operationID string, ok bool) {
	fields, hasFields := d.operationDestination()
	if !hasFields {
		return "", "", false
	}
	var operationKind, rawOperationID, boundProject string
	_ = json.Unmarshal(fields["operationKind"], &operationKind)
	_ = json.Unmarshal(fields["operationId"], &rawOperationID)
	_ = json.Unmarshal(fields["projectId"], &boundProject)
	if operationKind != "issue_create" {
		return "", "", false
	}
	if err := validProjectID(boundProject); err != nil {
		return "", "", false
	}
	id, err := normalizeOperationID(rawOperationID)
	if err != nil {
		return "", "", false
	}
	return boundProject, id, true
}

func (s *Service) resolveIssueCreationDestination(ctx context.Context, actor string, destination Destination) (*string, error) {
	projectID, creationOperationID, ok := destination.IssueCreationDestination()
	if !ok || s.issueCreationDestinationResolver == nil {
		return nil, nil
	}
	return s.issueCreationDestinationResolver(ctx, actor, projectID, creationOperationID)
}

func ParseDestination(data []byte) (Destination, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return Destination{}, failure(422, "VALIDATION_FAILED")
	}
	get := func(key string) string { var value string; _ = json.Unmarshal(fields[key], &value); return value }
	kind := get("kind")
	required := []string{"kind"}
	switch kind {
	case "home":
	case "project":
		required = append(required, "projectId")
	case "issue":
		required = append(required, "projectId", "issueId")
	case "conversation":
		required = append(required, "projectId", "conversationId")
	case "operation":
		required = append(required, "operationKind", "operationId")
		switch get("operationKind") {
		case "project_create", "provider_save", "model_test":
		case "project_update", "issue_create", "model_apply":
			required = append(required, "projectId")
		default:
			return Destination{}, failure(422, "VALIDATION_FAILED")
		}
	default:
		return Destination{}, failure(422, "VALIDATION_FAILED")
	}
	if len(fields) != len(required) {
		return Destination{}, failure(422, "VALIDATION_FAILED")
	}
	for _, key := range required {
		value := get(key)
		if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 || value == "." || value == ".." || strings.ContainsAny(value, "/%\\?#\x00") {
			return Destination{}, failure(422, "VALIDATION_FAILED")
		}
		for _, c := range value {
			if c < 32 || c == 127 {
				return Destination{}, failure(422, "VALIDATION_FAILED")
			}
		}
	}
	canonical, _ := json.Marshal(fields)
	return Destination{canonical: string(canonical), home: kind == "home"}, nil
}
