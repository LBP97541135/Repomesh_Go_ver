package scan

import (
	"sort"
	"strings"
)

// The alias registry and dependency graph are derived views over the catalog:
// never stored, rebuilt from the cards on demand (per the design doc, §1.7).
// One table stays one table.

// AliasRegistry resolves any identifier a repository might be called by —
// its authoritative platform name, build identities (Maven coordinates, npm
// package name…), deploy identities (compose service names, k8s labels) —
// to the repository that owns it. Lookup keys are lowercase.
//
// Collision rule: the authoritative platform name always wins over
// self-declared aliases; among aliases, the lexicographically first
// repository claims the name (deterministic, order-independent).
type AliasRegistry struct {
	byAlias map[string]RepositoryCard
	// Collisions lists identifiers claimed by more than one repository.
	// The registry resolves them by its priority rules, but a collision can
	// flip when the catalog changes, silently re-routing edges between
	// rebuilds — callers (scope checking) should surface these to operators.
	Collisions []string
}

// BuildAliasRegistry indexes the catalog for evidence resolution.
func BuildAliasRegistry(cards []RepositoryCard) AliasRegistry {
	registry := AliasRegistry{byAlias: map[string]RepositoryCard{}}
	sorted := append([]RepositoryCard(nil), cards...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	// Pass 1: authoritative names are unassailable.
	for _, card := range sorted {
		registry.byAlias[lower(card.Name)] = card
	}
	// Pass 2: self-declared aliases fill in everything not already claimed.
	claimedBy := func(key string, card RepositoryCard) bool {
		owner, ok := registry.byAlias[key]
		return ok && owner.ID == card.ID
	}
	for _, card := range sorted {
		if card.AutoCard == nil {
			continue
		}
		for _, alias := range append(append([]string(nil), card.AutoCard.Identities...), card.AutoCard.DeployIdentities...) {
			key := lower(alias)
			if key == "" {
				continue
			}
			if _, taken := registry.byAlias[key]; !taken {
				registry.byAlias[key] = card
				continue
			}
			if !claimedBy(key, card) {
				registry.Collisions = append(registry.Collisions, key)
			}
		}
	}
	sort.Strings(registry.Collisions)
	registry.Collisions = dedupeStrings(registry.Collisions)
	return registry
}

// Resolve maps an evidence identifier to the repository owning it.
func (r AliasRegistry) Resolve(name string) (RepositoryCard, bool) {
	card, ok := r.byAlias[lower(name)]
	return card, ok
}

// Edge is one resolved dependency: the From repository declared evidence
// naming To (or an alias of To). Self-edges and unresolvable names never
// become edges.
type Edge struct {
	FromID     string
	ToID       string
	ToName     string
	Mechanism  Mechanism
	Confidence Confidence
	// Calls is the observed call count; zero for edges without runtime
	// evidence (mechanism 6 import).
	Calls int
}

// Graph is the derived repository dependency graph.
type Graph struct {
	Edges []Edge
}

// BuildGraph resolves every card's evidence through the alias registry.
// ponytail: full rebuild per call — sub-second at the current catalog scale;
// add a per-process cache keyed on a catalog revision (invalidate on
// registration writes) if rebuild time is ever measured as a problem.
// Evidence naming an unscanned repository (an external library, or a repo
// nobody scanned) produces no edge — "unknown" is honest data.
func BuildGraph(cards []RepositoryCard, registry AliasRegistry) Graph {

	seen := map[string]bool{}
	var edges []Edge
	for _, card := range cards {
		for _, observed := range card.ObservedCalls {
			target, ok := registry.Resolve(observed.Target)
			if !ok || target.ID == card.ID {
				continue
			}
			key := lower(card.ID + "|" + target.ID + "|" + string(MechanismObserved))
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, Edge{
				FromID:     card.ID,
				ToID:       target.ID,
				ToName:     target.Name,
				Mechanism:  MechanismObserved,
				Confidence: ConfidenceConfirmed,
				Calls:      observed.Calls,
			})
		}
		if card.AutoCard == nil {
			continue
		}
		for _, evidence := range card.AutoCard.DepEvidence {
			target, ok := registry.Resolve(evidence.Name)
			if !ok || target.ID == card.ID {
				continue // unresolvable, or a repository referencing itself
			}
			key := lower(card.ID + "|" + target.ID + "|" + string(evidence.Mechanism))
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, Edge{
				FromID:     card.ID,
				ToID:       target.ID,
				ToName:     target.Name,
				Mechanism:  evidence.Mechanism,
				Confidence: evidence.Confidence,
			})
		}
	}
	return Graph{Edges: edges}
}

// Order returns the repositories in dependency order — a dependency before
// everything that depends on it — using confirmed edges only (declared ones
// are hints and must not dictate sequencing). Repositories on a cycle cannot
// be ordered and are reported separately, deterministic by name.
func (g Graph) Order(cards []RepositoryCard) (ordered, cyclic []string) {
	// Deterministic Kahn: always emit the alphabetically first ready node.
	remaining := map[string]bool{}
	for _, card := range cards {
		remaining[card.ID] = true
	}
	dependents := map[string]map[string]bool{} // to -> froms waiting on it
	pending := map[string]int{}                // from -> unresolved confirmed deps
	for _, edge := range g.Edges {
		if edge.Confidence != ConfidenceConfirmed {
			continue
		}
		if !remaining[edge.FromID] || !remaining[edge.ToID] {
			continue
		}
		if dependents[edge.ToID] == nil {
			dependents[edge.ToID] = map[string]bool{}
		}
		dependents[edge.ToID][edge.FromID] = true
		pending[edge.FromID]++
	}

	for len(remaining) > 0 {
		progressed := false
		for _, id := range sortedKeys(remaining) {
			if pending[id] == 0 {
				ordered = append(ordered, id)
				delete(remaining, id)
				for from := range dependents[id] {
					pending[from]--
				}
				progressed = true
				break
			}
		}
		if !progressed { // everything left sits on a cycle
			for id := range remaining {
				cyclic = append(cyclic, id)
			}
			sort.Strings(cyclic)
			return ordered, cyclic
		}
	}
	return ordered, cyclic
}

// Dependents returns the edges pointing at the target — the reverse
// dependencies that define a change's blast radius (重规划协议 §2 步骤 4b),
// deterministic by source repository.
func (g Graph) Dependents(targetID string) []Edge {
	var out []Edge
	for _, edge := range g.Edges {
		if edge.ToID == targetID {
			out = append(out, edge)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromID < out[j].FromID })
	return out
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lower(value string) string { return strings.ToLower(value) }

func dedupeStrings(values []string) []string {
	out := values[:0]
	previous := ""
	for i, value := range values {
		if i == 0 || value != previous {
			out = append(out, value)
		}
		previous = value
	}
	return out
}
