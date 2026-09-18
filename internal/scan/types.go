// Package scan implements repository scanning: evidence channels, the scan
// catalog, the dependency graph, and scope selection. Design doc:
// GOAI-infra-repomesh/docs/仓库扫描终版设计-Go蓝图与Python参照策略-2026-09-15.md
package scan

// Mechanism names how a dependency fact was proven. Only confirmed
// mechanisms participate in topological ordering.
type Mechanism string

const (
	MechanismBuild          Mechanism = "BUILD"
	MechanismRuntimeCall    Mechanism = "RUNTIME_CALL"
	MechanismSharedResource Mechanism = "SHARED_RESOURCE"
	MechanismDeploy         Mechanism = "DEPLOY"
	MechanismSource         Mechanism = "SOURCE"
	MechanismObserved       Mechanism = "OBSERVED"
)

// Confidence grades evidence: confirmed = the compiler/package manager
// executes it; declared = self-declared configuration, a hint only.
type Confidence string

const (
	ConfidenceConfirmed Confidence = "confirmed"
	ConfidenceDeclared  Confidence = "declared"
)

// DepEvidence is one dependency fact: who points at whom, proven by which
// mechanism, trusted how much.
type DepEvidence struct {
	Name       string     `json:"name"`
	Mechanism  Mechanism  `json:"mechanism"`
	Confidence Confidence `json:"confidence"`
}

// AutoCard is the structured snapshot of one repository produced by a scan.
// Field set mirrors the golden fixtures in the Python repo
// (tests/fixtures/golden_scan/*.expected.json); the Go port must reproduce
// those cards field for field.
type AutoCard struct {
	TopDirs          []string      `json:"topDirs"`
	Deps             []string      `json:"deps"`
	DepEvidence      []DepEvidence `json:"depEvidence"`
	Identities       []string      `json:"identities"`
	DeployIdentities []string      `json:"deployIdentities"`
	RecentCommits    []string      `json:"recentCommits"`
	ExposedAPIs      []string      `json:"exposedApis"`
	LowSignal        bool          `json:"lowSignal"`
}

// ScanStatus states how the scan that produced a card ended. Failed scans
// never enter the catalog as plausible empty cards.
type ScanStatus string

const (
	ScanStatusOK      ScanStatus = "ok"
	ScanStatusFailed  ScanStatus = "failed"
	ScanStatusSkipped ScanStatus = "skipped"
)

// RepositoryCard is one catalog row: the repository identity plus its latest
// scan result. Fingerprint (latest commit SHA at scan time) drives the
// incremental gate: a re-scan whose fingerprint is unchanged reuses the
// stored card instead of re-fetching and re-parsing. TestCommands/TestPaths
// are operator-owned; a re-scan refresh never overwrites them.
type RepositoryCard struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Topics      []string  `json:"topics"`
	Languages   []string  `json:"languages"`
	AutoCard    *AutoCard `json:"autoCard"`
	// ObservedCalls is mechanism 6 (runtime observation), imported one-shot
	// from the APM topology — never written by the channel scan, never
	// wiped by a scan refresh.
	ObservedCalls []ObservedCall `json:"observedCalls,omitempty"`
	ScanStatus    ScanStatus     `json:"scanStatus"`
	Fingerprint   string         `json:"fingerprint"`
	ProfiledAt    string         `json:"profiledAt"` // RFC3339 UTC
	TestCommands  []string       `json:"testCommands"`
	TestPaths     []string       `json:"testPaths"`
}
