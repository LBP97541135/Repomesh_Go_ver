package scan

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Mechanism ⑥ OBSERVED — runtime observation, imported one-shot from the
// target system's APM topology (design doc §5). Static evidence says "who
// declared dependence on whom"; observed evidence says "who actually called
// whom, how often". Four uses: delete zero-call fake edges, discover
// dynamically-routed edges, weight edges by real traffic, and mark
// validated pairs. The block lives on the caller's card under the
// metadata key observedCalls; scan refreshes never overwrite it.

// ObservedCall is one stored runtime call on a card: the callee service
// and the call count in the export window. The caller is the card itself.
type ObservedCall struct {
	Target string `json:"target"`
	Calls  int    `json:"calls"`
}

// ObservedTopologyCall is one call pair in the import file.
type ObservedTopologyCall struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Calls  int    `json:"calls"`
}

// ObservedTopology is the import file format, exported by the operator
// from the APM's governance API (no collector built here).
type ObservedTopology struct {
	Source      string                 `json:"source"`      // e.g. "skywalking"
	GeneratedAt string                 `json:"generatedAt"` // export timestamp
	Services    []string               `json:"services"`    // every service the APM saw, even uncalled
	Calls       []ObservedTopologyCall `json:"calls"`       // observed caller → callee pairs
}

// ObservedImportReport is what one import produced, including the two
// headline findings: static edges the traffic validated, and declared
// dependencies that never fired in the window (dead-dependency hints).
type ObservedImportReport struct {
	MappedCalls         int      `json:"mappedCalls"`
	UpdatedRepositories []string `json:"updatedRepositories"`
	UnmappedServices    []string `json:"unmappedServices"`
	ValidatedEdges      []string `json:"validatedEdges"`
	ZeroCallHints       []string `json:"zeroCallHints"`
}

// ImportObserved maps an APM topology onto the catalog through the alias
// registry and persists OBSERVED evidence on each caller's card. Service
// names resolve through the same registry as static evidence; services
// that resolve to nothing are reported, never guessed.
func ImportObserved(ctx context.Context, store CatalogStore, registry AliasRegistry, topology ObservedTopology) (ObservedImportReport, error) {
	report := ObservedImportReport{}

	cards, err := store.List(ctx)
	if err != nil {
		return report, err
	}
	byID := make(map[string]RepositoryCard, len(cards))
	for _, card := range cards {
		byID[card.ID] = card
	}

	// Which catalog repositories the APM actually saw (any of their
	// aliases appearing in the topology counts).
	repoSeen := map[string]bool{}
	var unmapped []string
	unmappedSeen := map[string]bool{}
	noteUnmapped := func(service string) {
		service = strings.TrimSpace(service)
		if service == "" {
			return
		}
		key := strings.ToLower(service)
		if unmappedSeen[key] {
			return
		}
		unmappedSeen[key] = true
		unmapped = append(unmapped, service)
	}
	for _, service := range topology.Services {
		if card, ok := registry.Resolve(service); ok {
			repoSeen[card.ID] = true
		} else {
			noteUnmapped(service)
		}
	}

	// Resolve every observed call to a repo pair.
	type observedCall struct {
		targetID   string
		targetName string
		calls      int
	}
	byRepo := map[string][]observedCall{}
	observedPair := map[string]int{}
	for _, call := range topology.Calls {
		source, sourceOK := registry.Resolve(strings.TrimSpace(call.Source))
		target, targetOK := registry.Resolve(strings.TrimSpace(call.Target))
		if !sourceOK {
			noteUnmapped(call.Source)
			continue
		}
		if !targetOK {
			noteUnmapped(call.Target)
			continue
		}
		if source.ID == target.ID {
			continue // a service calling itself is not a dependency
		}
		byRepo[source.ID] = append(byRepo[source.ID], observedCall{
			targetID: target.ID, targetName: target.Name, calls: call.Calls,
		})
		observedPair[edgeKey(source.ID, target.ID)] += call.Calls
		report.MappedCalls++
	}
	for _, service := range unmapped {
		report.UnmappedServices = append(report.UnmappedServices, service)
	}
	sort.Strings(report.UnmappedServices)

	// Persist one observed block per affected caller card, idempotent.
	ids := make([]string, 0, len(byRepo))
	for id := range byRepo {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		calls := byRepo[id]
		sort.Slice(calls, func(i, j int) bool { return calls[i].targetID < calls[j].targetID })
		observed := make([]ObservedCall, 0, len(calls))
		for _, call := range calls {
			observed = append(observed, ObservedCall{Target: call.targetName, Calls: call.calls})
		}
		if err := store.ReplaceObservedCalls(ctx, id, observed); err != nil {
			return report, err
		}
		if card, ok := byID[id]; ok {
			report.UpdatedRepositories = append(report.UpdatedRepositories, card.Name)
		}
	}
	sort.Strings(report.UpdatedRepositories)

	// Judge static confirmed edges against the traffic: validated when the
	// pair fired, a zero-call hint when both endpoints were observed but
	// the pair never did. Edges whose endpoints sit outside the observed
	// set are left unjudged.
	graph := BuildGraph(cards, registry)
	for _, edge := range graph.Edges {
		if edge.Confidence != ConfidenceConfirmed {
			continue
		}
		key := edgeKey(edge.FromID, edge.ToID)
		calls, fired := observedPair[key]
		if !repoSeen[edge.FromID] && !repoSeen[edge.ToID] {
			continue // this pair was never under observation
		}
		if fired {
			report.ValidatedEdges = append(report.ValidatedEdges,
				fmt.Sprintf("%s -> %s (%d calls)", edge.FromID, edge.ToName, calls))
			continue
		}
		if repoSeen[edge.FromID] && repoSeen[edge.ToID] {
			report.ZeroCallHints = append(report.ZeroCallHints,
				fmt.Sprintf("%s -> %s", edge.FromID, edge.ToName))
		}
	}
	sort.Strings(report.ValidatedEdges)
	sort.Strings(report.ZeroCallHints)
	return report, nil
}

func edgeKey(fromID, toID string) string { return fromID + "|" + toID }
