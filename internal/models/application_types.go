package models

import (
	"encoding/json"
	"time"

	"repomesh.local/repomesh/internal/jsoninput"
	"repomesh.local/repomesh/internal/projects"
)

// OriginalModel is the authorized projection of the project's current model
// binding. Unknown only says authorization failed; restricted lists reasons
// without revealing the binding.
type OriginalModel interface{ originalModel() }

type UnconfiguredOriginal struct{}

func (UnconfiguredOriginal) originalModel() {}

type ReadableOriginal struct {
	Binding      projects.ModelBinding
	Reference    projects.ProfileChoice
	SnapshotPath string
}

func (ReadableOriginal) originalModel() {}

type RestrictedOriginal struct {
	Reasons []string
}

func (RestrictedOriginal) originalModel() {}

// ApplicationPreview is the read-only apply preview. Before is never
// serialized; the original field carries the already-authorized projection.
type ApplicationPreview struct {
	ID                    string
	Actor                 string
	ProjectID             string
	ProjectRevision       string
	Before                projects.FixedConfiguration
	original              OriginalModel
	candidateModelID      string
	Candidate             projects.ModelBinding
	configurationRevision string
	Target                SnapshotTarget
	MaxOutputTokens       int64
	ExpiresAt             time.Time
	CanApply              bool
	Reasons               []string
}

type originalModelJSON struct {
	State        string            `json:"state"`
	Reasons      []string          `json:"reasons,omitempty"`
	Binding      *modelBindingJSON `json:"binding,omitempty"`
	SnapshotPath string            `json:"snapshotPath,omitempty"`
}

type modelBindingJSON struct {
	ProfileID        string `json:"profileId"`
	ProfileVersion   string `json:"profileVersion"`
	ProviderID       string `json:"providerId"`
	ProviderRevision string `json:"providerRevision"`
	ModelRowID       string `json:"modelRowId"`
}

type applicationPreviewJSON struct {
	PreviewID        string             `json:"previewId"`
	ProjectID        string             `json:"projectId"`
	ProjectRevision  string             `json:"projectRevision"`
	CandidateModelID string             `json:"candidateModelId"`
	Target           snapshotTargetJSON `json:"target"`
	ExpiresAt        time.Time          `json:"expiresAt"`
	CanApply         bool               `json:"canApply"`
	Reasons          []string           `json:"reasons"`
	Original         originalModelJSON  `json:"original"`
}

func (p ApplicationPreview) MarshalJSON() ([]byte, error) {
	wire := applicationPreviewJSON{
		PreviewID:        p.ID,
		ProjectID:        p.ProjectID,
		ProjectRevision:  p.ProjectRevision,
		CandidateModelID: p.candidateModelID,
		Target:           snapshotTargetJSON(p.Target),
		ExpiresAt:        p.ExpiresAt,
		CanApply:         p.CanApply,
		Reasons:          reasonsOrEmpty(p.Reasons),
		Original:         marshalOriginal(p.original),
	}
	return json.Marshal(wire)
}

func marshalOriginal(value OriginalModel) originalModelJSON {
	switch original := value.(type) {
	case UnconfiguredOriginal:
		return originalModelJSON{State: "unconfigured"}
	case ReadableOriginal:
		return originalModelJSON{State: "readable", Binding: &modelBindingJSON{
			ProfileID:        original.Binding.Profile.ID,
			ProfileVersion:   original.Binding.Profile.Version,
			ProviderID:       original.Binding.ProviderID,
			ProviderRevision: original.Binding.ProviderRevision,
			ModelRowID:       original.Binding.ModelRowID,
		}, SnapshotPath: original.SnapshotPath}
	case RestrictedOriginal:
		return originalModelJSON{State: "restricted", Reasons: reasonsOrEmpty(original.Reasons)}
	}
	return originalModelJSON{State: "restricted", Reasons: []string{"original_model_unknown"}}
}

// ApplicationCommand is the parsed apply command. Key is the client-chosen
// application id and doubles as application_operations.application_id.
type ApplicationCommand struct {
	projectID string
	key       string
	previewID string
	requestID string
}

// ParseApplicationCommand validates the apply body.
func ParseApplicationCommand(projectID string, key string, requestID string, data []byte) (ApplicationCommand, error) {
	if !validUUID(normalizeKey(projectID)) {
		return ApplicationCommand{}, validation("projectId")
	}
	if len(key) < 1 || len(key) > 128 {
		return ApplicationCommand{}, validation("applicationId")
	}
	var wire struct {
		PreviewID string `json:"previewId"`
	}
	if err := jsoninput.Decode(data, &wire, 256*1024); err != nil {
		return ApplicationCommand{}, validation("body")
	}
	if !validUUID(wire.PreviewID) {
		return ApplicationCommand{}, validation("previewId")
	}
	if len(requestID) < 1 || len(requestID) > 128 {
		return ApplicationCommand{}, validation("requestId")
	}
	return ApplicationCommand{projectID: normalizeKey(projectID), key: normalizeKey(key), previewID: wire.PreviewID, requestID: requestID}, nil
}

// ApplicationReceipt is the closed outcome of one apply operation.
type ApplicationReceipt interface {
	applicationReceipt()
}

type Applied struct {
	ApplicationID         string
	ProjectID             string
	ProjectRevision       string
	ConfigurationRevision projects.ConfigurationRevision
	Candidate             projects.ModelBinding
	RetainedExecution     projects.ProfileVersionRef
	CommittedAt           time.Time
}

type appliedJSON struct {
	ApplicationID         string           `json:"applicationId"`
	ProjectID             string           `json:"projectId"`
	ProjectRevision       string           `json:"projectRevision"`
	ConfigurationRevision string           `json:"configurationRevision"`
	Candidate             modelBindingJSON `json:"candidate"`
	RetainedExecution     string           `json:"retainedExecution"`
	CommittedAt           time.Time        `json:"committedAt"`
}

func (a Applied) MarshalJSON() ([]byte, error) {
	return json.Marshal(appliedJSON{
		ApplicationID:         a.ApplicationID,
		ProjectID:             a.ProjectID,
		ProjectRevision:       a.ProjectRevision,
		ConfigurationRevision: string(a.ConfigurationRevision),
		Candidate: modelBindingJSON{
			ProfileID:        a.Candidate.Profile.ID,
			ProfileVersion:   a.Candidate.Profile.Version,
			ProviderID:       a.Candidate.ProviderID,
			ProviderRevision: a.Candidate.ProviderRevision,
			ModelRowID:       a.Candidate.ModelRowID,
		},
		RetainedExecution: a.RetainedExecution.ID + "@" + a.RetainedExecution.Version,
		CommittedAt:       a.CommittedAt,
	})
}

// ApplicationRejected is the receipt for a preview that no longer holds.
type ApplicationRejected struct {
	ApplicationID string
	Error         APIError
	DecidedAt     time.Time
}

type applicationRejectedJSON struct {
	ApplicationID string    `json:"applicationId"`
	Error         APIError  `json:"error"`
	DecidedAt     time.Time `json:"decidedAt"`
}

func (a ApplicationRejected) MarshalJSON() ([]byte, error) {
	return json.Marshal(applicationRejectedJSON{a.ApplicationID, a.Error, a.DecidedAt})
}

func (ApplicationRejected) applicationReceipt() {}
func (Applied) applicationReceipt()             {}

// ApplicationView is the read projection of one application operation.
type ApplicationView struct {
	ApplicationID         string
	ProjectID             string
	RequestID             string
	Outcome               string
	RejectionCode         string
	Before                originalModelJSON
	Candidate             modelBindingJSON
	ConfigurationRevision *string
	RetainedExecution     *string
	CommittedAt           time.Time
	RemovedAt             *time.Time
}
