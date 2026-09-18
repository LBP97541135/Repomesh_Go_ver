package scan

// FileEntry is one row of a repository file tree. Deliberately decoupled
// from any platform client: the fetch layer maps its own types onto this.
type FileEntry struct {
	Path  string
	IsDir bool
}

// ChannelOutput is what one evidence channel contributes for one file.
// Empty output is always legal: a file a channel cannot parse must never
// fail a scan.
type ChannelOutput struct {
	Deps             []string      // free-text dependency names (keyword matching)
	Evidence         []DepEvidence // structured facts for the graph
	Identifiers      []string      // names this repository declares for itself
	DeployIdentities []string      // service names this repository deploys as
	ExposedAPIs      []string      // routes, card metadata only — never evidence
}

// EvidenceChannel is the contract every scanner channel implements. The scan
// service walks the registered channel table; adding a channel or an
// ecosystem parser means registering, not editing the core.
type EvidenceChannel interface {
	// Name returns the channel id (also the dedupe namespace for evidence).
	Name() string
	// Select picks the files this channel wants from the full tree.
	Select(tree []FileEntry) []string
	// Parse turns one fetched file into channel output.
	Parse(filename, content string) ChannelOutput
}
