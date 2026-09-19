package observepipe

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"

	"repomesh.local/repomesh/internal/observability"
)

type FactSource interface {
	ListFacts(context.Context, string, string) ([]observability.Fact, error)
}
type CollectSummary struct {
	SourceFacts int    `json:"source_facts"`
	Added       int    `json:"added"`
	Existing    int    `json:"existing"`
	Coverage    string `json:"coverage"`
}

// CollectDiscovery reads immutable committed facts, not a mutable current-state
// row. It re-scans this exact scope and deduplicates by source identity, so a
// transaction with a lower ID that commits late remains discoverable.
func CollectDiscoveryFrom(ctx context.Context, source FactSource, j *Journal, sourceID, project, issue string) (CollectSummary, error) {
	out := CollectSummary{Coverage: "discovery_history_only"}
	if sourceID == "" || project == "" || issue == "" {
		return out, errors.New("source deployment and exact project and issue IDs are required")
	}
	facts, err := source.ListFacts(ctx, project, issue)
	if err != nil {
		return out, errors.New("cannot read committed discovery facts; check source schema and scope")
	}
	for _, fact := range facts {
		if fact.SourceVersion != observability.DiscoverySourceVersion {
			return out, errors.New("unsupported fact source version; no guessed conversion")
		}
		if fact.ProjectID != project || fact.IssueID != issue || fact.ID == "" || Digest(fact.Snapshot) != fact.Fingerprint {
			return out, errors.New("source fact identity or content integrity mismatch")
		}
		var body struct {
			SchemaVersion string                     `json:"schema_version"`
			ProjectID     string                     `json:"project_id"`
			IssueID       string                     `json:"issue_id"`
			Candidates    map[string]json.RawMessage `json:"candidates"`
		}
		if json.Unmarshal(fact.Snapshot, &body) != nil || body.SchemaVersion != fact.SourceVersion || body.ProjectID != project || body.IssueID != issue {
			return out, errors.New("fact snapshot lineage is invalid")
		}
		ref, err := j.PutRawJSON(fact.Snapshot)
		if err != nil {
			return out, err
		}
		manifest, err := j.PutJSON(map[string]any{"schema_version": "repomesh-context/0.1", "source_deployment_id": sourceID, "source_kind": "committed_discovery_snapshot", "source_version": fact.SourceVersion, "source_fact_id": fact.ID, "source_fingerprint": fact.Fingerprint, "project_id": project, "issue_id": issue, "snapshot_ref": ref, "product_build_status": "not_recorded", "coverage": "discovery_history_only"})
		if err != nil {
			return out, err
		}
		e := NewEvent("repomesh-discovery", fact.SourceVersion, sourceID+"/"+project+"/"+issue+"/fact/"+fact.ID, "discovery.snapshot.committed", sourceID+":issue:"+issue, fact.OccurredAt)
		e.ProjectID = project
		e.IssueID = issue
		e.ContextManifestRef = manifest
		e.EvidenceRefs = []string{ref}
		e.InputArtifactRefs = []string{ref}
		e.MissingFields = []string{"runtime_context", "model_usage", "product_build", "candidate_combination"}
		if len(body.Candidates) > 0 {
			if _, ok := body.Candidates["input_pool"]; !ok {
				e.MissingFields = append(e.MissingFields, "selection_input_pool")
			}
		}
		e.MissingReason = StringPtr("This source proves committed discovery state only; other stages were not collected.")
		_, added, err := j.Append(e)
		if err != nil {
			return out, err
		}
		out.SourceFacts++
		if added {
			out.Added++
		} else {
			out.Existing++
		}
	}
	return out, nil
}

func CollectorBuild() map[string]string {
	out := map[string]string{"version": "dev"}
	if b, ok := debug.ReadBuildInfo(); ok {
		for _, s := range b.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.modified" {
				out[s.Key] = s.Value
			}
		}
	}
	return out
}
