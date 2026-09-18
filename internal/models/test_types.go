package models

import (
	"encoding/json"
	"time"

	"repomesh.local/repomesh/internal/jsoninput"
	"repomesh.local/repomesh/internal/projects"
)

// SnapshotTarget names the exact provider snapshot a test or application is
// pinned to. All three columns must exist in repomesh_models.model_snapshots.
type SnapshotTarget struct {
	ProviderID       string
	ProviderRevision string
	ModelRowID       string
}

// TestPolicy is the exact policy combination locked at preview time. It
// travels unchanged through registration and dispatch; nothing is re-resolved
// later.
type TestPolicy struct {
	Revision         string
	Budget           projects.RequestPolicy
	Limits           projects.TimePolicy
	EgressID         string
	EgressVersion    string
	ApprovedBaseURLs []string
	AllowPrivateAddr bool
	FollowRedirects  bool
}

// TestPreview is the read-only projection returned before registration. The
// MarshalJSON whitelist below never encodes the actor, the full model binding,
// the policy combination, or the approved base URLs.
type TestPreview struct {
	ID                  string
	Actor               string
	Target              SnapshotTarget
	Model               projects.ModelBinding
	ModelID             string
	Policy              TestPolicy
	MaxOutputTokens     int64
	ExpiresAt           time.Time
	CanSubmit           bool
	Reasons             []string
	ExistingTestID      *string
	ExistingOutstanding bool
}

type testPreviewJSON struct {
	PreviewID        string              `json:"previewId"`
	ProviderID       string              `json:"providerId"`
	ProviderRevision string              `json:"providerRevision"`
	ModelRowID       string              `json:"modelRowId"`
	ModelID          string              `json:"modelId"`
	MaxOutputTokens  int64               `json:"maxOutputTokens"`
	ExpiresAt        time.Time           `json:"expiresAt"`
	CanSubmit        bool                `json:"canSubmit"`
	Reasons          []string            `json:"reasons,omitempty"`
	ExistingTestID   string              `json:"existingTestId,omitempty"`
	Links            *TestOperationLinks `json:"links,omitempty"`
}

func (p TestPreview) MarshalJSON() ([]byte, error) {
	wire := testPreviewJSON{
		PreviewID:        p.ID,
		ProviderID:       p.Target.ProviderID,
		ProviderRevision: p.Target.ProviderRevision,
		ModelRowID:       p.Target.ModelRowID,
		ModelID:          p.ModelID,
		MaxOutputTokens:  p.MaxOutputTokens,
		ExpiresAt:        p.ExpiresAt,
		CanSubmit:        p.CanSubmit,
		Reasons:          reasonsOrEmpty(p.Reasons),
	}
	if p.ExistingTestID != nil && p.ExistingOutstanding {
		wire.ExistingTestID = *p.ExistingTestID
		wire.Links = &TestOperationLinks{Operation: "/api/model-tests/" + *p.ExistingTestID}
	}
	return json.Marshal(wire)
}

func reasonsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// TestOperationLinks is the HAL-style self link for an outstanding test.
type TestOperationLinks struct {
	Operation string `json:"operation"`
}

// TestCommand is the parsed registration command. Key is the client-chosen
// test id and doubles as tests.test_id for replay.
type TestCommand struct {
	key                    string
	previewID              string
	confirmPotentialCharge bool
}

// ParseTestCommand validates the registration body. maxOutputTokens must
// match the preview's locked value exactly.
func ParseTestCommand(key string, data []byte) (TestCommand, error) {
	if !validUUID(normalizeKey(key)) {
		return TestCommand{}, validation("testId")
	}
	var wire struct {
		PreviewID              string `json:"previewId"`
		ConfirmPotentialCharge bool   `json:"confirmPotentialCharge"`
	}
	if err := jsoninput.Decode(data, &wire, 256*1024); err != nil {
		return TestCommand{}, validation("body")
	}
	if !validUUID(wire.PreviewID) {
		return TestCommand{}, validation("previewId")
	}
	return TestCommand{key: normalizeKey(key), previewID: wire.PreviewID, confirmPotentialCharge: wire.ConfirmPotentialCharge}, nil
}

// TestSubmitFailure carries the outstanding-test details that the web layer
// forwards verbatim when a duplicate test id collides.
type TestSubmitFailure struct {
	Status      int
	Code        string
	FieldErrors []FieldError
	Details     *TestOutstandingDetails
}

func (e *TestSubmitFailure) Error() string { return e.Code }

func (e TestSubmitFailure) MarshalJSON() ([]byte, error) {
	wire := struct {
		Code        string                  `json:"code"`
		FieldErrors []FieldError            `json:"fieldErrors,omitempty"`
		Details     *TestOutstandingDetails `json:"details,omitempty"`
	}{e.Code, e.FieldErrors, e.Details}
	return json.Marshal(wire)
}

// TestOutstandingDetails names the already-registered test that blocks a
// duplicate submission.
type TestOutstandingDetails struct {
	ExistingTestID string             `json:"existingTestId"`
	Links          TestOperationLinks `json:"links"`
}

// TestResult is the read projection of one registered test.
type TestResult struct {
	ID             string
	Actor          string
	Target         SnapshotTarget
	ModelID        string
	AcceptedAt     time.Time
	ResultRevision string
	State          string
	Observation    *DispatchObservation
	BudgetStatus   string
	Recovery       string
	RemovedAt      *time.Time
}

type testResultJSON struct {
	ID             string               `json:"id"`
	Target         snapshotTargetJSON   `json:"target"`
	ModelID        string               `json:"modelId"`
	AcceptedAt     time.Time            `json:"acceptedAt"`
	ResultRevision string               `json:"resultRevision"`
	State          string               `json:"state"`
	Observation    *DispatchObservation `json:"observation,omitempty"`
	BudgetStatus   string               `json:"budgetStatus"`
	Recovery       string               `json:"recovery"`
}

func (r TestResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(testResultJSON{
		ID:             r.ID,
		Target:         snapshotTargetJSON(r.Target),
		ModelID:        r.ModelID,
		AcceptedAt:     r.AcceptedAt,
		ResultRevision: r.ResultRevision,
		State:          r.State,
		Observation:    r.Observation,
		BudgetStatus:   r.BudgetStatus,
		Recovery:       r.Recovery,
	})
}

type snapshotTargetJSON struct {
	ProviderID       string `json:"providerId"`
	ProviderRevision string `json:"providerRevision"`
	ModelRowID       string `json:"modelRowId"`
}

// DispatchObservation is the single latest observation attached to a test
// result projection.
type DispatchObservation struct {
	EvidenceID          string
	ActorID             string
	TestID              string
	ExternalOperationID string
	Generation          int64
	StartedAt           *time.Time
	ObservedAt          time.Time
	Code                string
	LatencyMS           *int64
	Usage               *TokenUsage
}

type dispatchObservationJSON struct {
	EvidenceID          string      `json:"evidenceId"`
	ExternalOperationID string      `json:"externalOperationId"`
	StartedAt           *time.Time  `json:"startedAt,omitempty"`
	ObservedAt          time.Time   `json:"observedAt"`
	Code                string      `json:"code"`
	LatencyMS           *int64      `json:"latencyMs,omitempty"`
	Usage               *TokenUsage `json:"usage,omitempty"`
}

func (o DispatchObservation) MarshalJSON() ([]byte, error) {
	return json.Marshal(dispatchObservationJSON{
		EvidenceID:          o.EvidenceID,
		ExternalOperationID: o.ExternalOperationID,
		StartedAt:           o.StartedAt,
		ObservedAt:          o.ObservedAt,
		Code:                o.Code,
		LatencyMS:           o.LatencyMS,
		Usage:               o.Usage,
	})
}

// TokenUsage is the observed token pair. Both fields are set together or
// neither (enforced by the table CHECK).
type TokenUsage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

// TestClaim is one short lease taken by a runner loop before dispatch work.
type TestClaim struct {
	testID     string
	owner      string
	generation int64
	leaseUntil time.Time
}

// CredentialCapabilityRef names the exact provider credential version a
// dispatch was authorized against.
type CredentialCapabilityRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// SendPermit is the in-memory authorization created only after the permit
// transaction commits. It is never persisted and cleared after one use.
type SendPermit struct {
	actorID             string
	testID              string
	externalOperationID string
	permitID            string
	nonce               string
	credential          CredentialCapabilityRef
	consumed            bool
}
