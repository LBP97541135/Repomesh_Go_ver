package sources

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/projects"
)

type EnvironmentTemplate struct {
	ID                     string `json:"id"`
	Version                string `json:"version"`
	ExecutorPoolID         string `json:"executorPoolId"`
	ApprovedTemplateDigest string `json:"approvedTemplateDigest"`
	NetworkPolicyRef       string `json:"networkPolicyRef"`
	ResourceClassID        string `json:"resourceClassId"`
	Enabled                bool   `json:"enabled"`
}

type ExecutionProfile struct {
	ID                       string `json:"id"`
	Version                  string `json:"version"`
	OwnerID                  string `json:"ownerId"`
	Name                     string `json:"name"`
	TemplateID               string `json:"templateId"`
	TemplateVersion          string `json:"templateVersion"`
	WorkerConcurrency        int    `json:"workerConcurrency"`
	VerificationGroupEnabled bool   `json:"verificationGroupEnabled"`
}

type DefaultBinding struct {
	Kind           string `json:"kind"`
	OwnerID        string `json:"ownerId"`
	ProfileID      string `json:"profileId"`
	ProfileVersion string `json:"profileVersion"`
}

type Manifest struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	ImportID             string                `json:"importId"`
	EnvironmentTemplates []EnvironmentTemplate `json:"environmentTemplates"`
	ExecutionProfiles    []ExecutionProfile    `json:"executionProfiles"`
	DefaultBindings      []DefaultBinding      `json:"defaultBindings"`
}

func (Manifest) importPayload() {}

// EgressPolicy is one schema2 outbound allowlist version. Registration never
// performs DNS or HTTP probes.
type EgressPolicy struct {
	ID, Version           string
	ApprovedBaseURLs      []string
	AllowedPort           int
	AllowPrivateAddresses bool
	FollowRedirects       bool
}

// TestBinding pins one owner to one exact combination of three policy versions.
type TestBinding struct {
	OwnerID string
	Budget  projects.PolicyRef
	Limits  projects.PolicyRef
	Egress  projects.PolicyRef
}

// CompleteExecution is one schema2 execution version: the schema1 identity plus
// the two pinned policy refs. The same id/version as a schema1 record must not
// be completed in place; a new version is required.
type CompleteExecution struct {
	Identity ExecutionProfile
	Budget   projects.PolicyRef
	Limits   projects.PolicyRef
}

// ManifestCommon holds the declarations shared with schema1 but is not a wire
// key: the schema2 wire format keeps one flat executionProfiles array.
type ManifestCommon struct {
	SchemaVersion        int
	ImportID             string
	EnvironmentTemplates []EnvironmentTemplate
	DefaultBindings      []DefaultBinding
}

// ManifestV2 is normalized internal data. Its flat wire schema has exactly one
// executionProfiles array. Common is not a wire key, and parseImportV2 retains
// canonical bytes for exact replay.
type ManifestV2 struct {
	Common            ManifestCommon
	ExecutionProfiles []CompleteExecution
	Budgets           []projects.RequestPolicy
	Limits            []projects.TimePolicy
	Egress            []EgressPolicy
	Tests             []TestBinding
}

func (ManifestV2) importPayload() {}

// importPayload marks one parsed import payload version. ParseImport's
// existing signature dispatches the version and constructs exactly one payload.
type importPayload interface {
	importPayload()
}

type ImportCommand struct {
	payload       importPayload
	canonical     []byte
	schemaVersion int
}

type VersionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type DefaultResult struct {
	OwnerID         string `json:"ownerId"`
	ProfileID       string `json:"profileId"`
	ProfileVersion  string `json:"profileVersion"`
	DefaultRevision string `json:"defaultRevision"`
}

// PolicyImportResults is the schema2 receipt addendum: the four arrays are
// returned even when empty. There is no "policies" wire key, and schema1
// receipts keep their exact original shape.
type PolicyImportResults struct {
	BudgetPolicies    []VersionRef
	TimeLimitPolicies []VersionRef
	EgressPolicies    []VersionRef
	TestBindings      []TestBindingResult
}

type TestBindingResult struct {
	OwnerID  string
	Revision string
}

type Receipt struct {
	ImportID             string          `json:"importId"`
	SchemaVersion        int             `json:"schemaVersion"`
	EnvironmentTemplates []VersionRef    `json:"environmentTemplates"`
	ExecutionProfiles    []VersionRef    `json:"executionProfiles"`
	DefaultBindings      []DefaultResult `json:"defaultBindings"`
	CommittedAt          time.Time       `json:"committedAt"`
	policyResults        *PolicyImportResults
}

// MarshalJSON appends the four schema2 arrays only when policyResults is set;
// the schema1 branch preserves the exact original receipt shape.
func (r Receipt) MarshalJSON() ([]byte, error) {
	base := struct {
		ImportID             string          `json:"importId"`
		SchemaVersion        int             `json:"schemaVersion"`
		EnvironmentTemplates []VersionRef    `json:"environmentTemplates"`
		ExecutionProfiles    []VersionRef    `json:"executionProfiles"`
		DefaultBindings      []DefaultResult `json:"defaultBindings"`
		CommittedAt          time.Time       `json:"committedAt"`
	}{r.ImportID, r.SchemaVersion, r.EnvironmentTemplates, r.ExecutionProfiles, r.DefaultBindings, r.CommittedAt}
	if r.policyResults == nil {
		return json.Marshal(base)
	}
	empty := func(refs []VersionRef) []VersionRef {
		if refs == nil {
			return []VersionRef{}
		}
		return refs
	}
	emptyBindings := func(bindings []TestBindingResult) []TestBindingResult {
		if bindings == nil {
			return []TestBindingResult{}
		}
		return bindings
	}
	results := *r.policyResults
	return json.Marshal(struct {
		ImportID             string              `json:"importId"`
		SchemaVersion        int                 `json:"schemaVersion"`
		EnvironmentTemplates []VersionRef        `json:"environmentTemplates"`
		ExecutionProfiles    []VersionRef        `json:"executionProfiles"`
		DefaultBindings      []DefaultResult     `json:"defaultBindings"`
		CommittedAt          time.Time           `json:"committedAt"`
		BudgetPolicies       []VersionRef        `json:"budgetPolicies"`
		TimeLimitPolicies    []VersionRef        `json:"timeLimitPolicies"`
		EgressPolicies       []VersionRef        `json:"egressPolicies"`
		TestBindings         []TestBindingResult `json:"testBindings"`
	}{base.ImportID, base.SchemaVersion, base.EnvironmentTemplates, base.ExecutionProfiles, base.DefaultBindings,
		base.CommittedAt, empty(results.BudgetPolicies), empty(results.TimeLimitPolicies),
		empty(results.EgressPolicies), emptyBindings(results.TestBindings)})
}

type ImportResult struct {
	Receipt  Receipt
	Replayed bool
}

type DeploymentPrincipal struct {
	deploymentID string
	sessionUser  string
}

type Importer struct {
	pool      *pgxpool.Pool
	principal DeploymentPrincipal
	catalog   *projects.CatalogWriter
	hook      importHook
}

type Failure struct {
	Status int
	Code   string
}

func (e *Failure) Error() string { return e.Code }

type importPhase string

const (
	ownersLocked              importPhase = "owners_locked"
	importLocked              importPhase = "import_locked"
	templatesInserted         importPhase = "templates_inserted"
	executionProfilesInserted importPhase = "execution_profiles_inserted"
	defaultsBound             importPhase = "defaults_bound"
	importReceiptInserted     importPhase = "import_receipt_inserted"
	importBeforeCommit        importPhase = "import_before_commit"
)

type importHook func(context.Context, importPhase) error

func failure(status int, code string) error { return &Failure{Status: status, Code: code} }
func unavailable() error                    { return failure(503, "RESULT_UNCONFIRMED") }
