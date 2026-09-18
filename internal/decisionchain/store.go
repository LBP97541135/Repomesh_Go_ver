package decisionchain

import (
	"context"
	"time"
)

// nodeWrite is the store-level insert shape: everything decided, nothing
// left for the store to interpret.
type nodeWrite struct {
	EventID              string
	RequirementText      string // normalized
	RequirementKey       string
	ProjectID            string // A套 project uuid; "" = NULL
	Step                 DecisionStep
	Status               DecisionStatus
	ActorType            string // llm|human|service
	ActorID              string
	ParentNodeID         string // upstream decision node id, "" = chain root
	Action               string
	Rationale            string
	ContextRef           map[string]any
	AffectedRepositories []string
	Source               NodeSource
}

// Filter narrows List. Zero fields mean "no constraint".
type Filter struct {
	RequirementKey string
	ProjectID      string // exact uuid match on project_id
	Step           *DecisionStep
	Repository     string // matches one affected repository name
	Keyword        string // substring match on requirement_text
	Limit          int    // default 50, hard cap 200
	Offset         int
}

// ScoredNode pairs a node with its recall score and, for structural hits,
// the repository names that intersected.
type ScoredNode struct {
	Node                DecisionNode `json:"node"`
	Score               float64      `json:"score"`
	MatchedRepositories []string     `json:"matchedRepositories,omitempty"`
}

// Store persists decision nodes and their embeddings. The service depends on
// this interface; PostgreSQL implements it (and tests fake it).
type Store interface {
	// Record inserts one node. Idempotent by EventID: a redelivery returns
	// the stored node unchanged. Version = max(version)+1 per
	// (requirement_key, step), unique-index backed (F9).
	Record(ctx context.Context, w nodeWrite) (DecisionNode, error)
	List(ctx context.Context, f Filter) ([]DecisionNode, error)
	Get(ctx context.Context, id string) (*DecisionNode, error)

	// PendingEmbeddings returns nodes lacking a current-model embedding row
	// (never embedded, or embedded with a different model — F5).
	PendingEmbeddings(ctx context.Context, model string, limit int) ([]DecisionNode, error)
	// UpsertEmbedding writes the vector row: primary vector column + JSON
	// fallback copy in one write (D11).
	UpsertEmbedding(ctx context.Context, nodeID, model string, vec []float32, embeddedAt time.Time) error

	// SemanticCandidates orders current-model embeddings by cosine distance
	// to query (pgvector path); score = 1 - distance.
	SemanticCandidates(ctx context.Context, model string, query []float32, limit int) ([]ScoredNode, error)
	// StructuralCandidates prescreens nodes sharing any repository name
	// (GIN-backed); Jaccard is computed by the caller.
	StructuralCandidates(ctx context.Context, names []string, limit int) ([]DecisionNode, error)

	// FeatureEnabled returns the toggle; a missing row counts as enabled.
	FeatureEnabled(ctx context.Context, feature string) (bool, error)
	// SetFeature writes the toggle with its audit fields.
	SetFeature(ctx context.Context, feature string, enabled bool, updatedBy string, at time.Time) error
}
