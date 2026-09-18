package scan

import (
	"sort"
	"strings"
)

// BuildChannel is mechanism ① BUILD: build manifests name the dependencies
// the package manager will actually fetch, so their evidence is confirmed,
// and the manifest's own identity registers the repository's aliases.
type BuildChannel struct{}

// NewBuildChannel returns the channel ready for RegisterChannel.
func NewBuildChannel() BuildChannel { return BuildChannel{} }

// Name is the channel id and the dedupe namespace for its evidence.
func (BuildChannel) Name() string { return "BUILD" }

// Limits copied from the Python original: at most N manifests of any one
// kind and M per repository, so a monorepo full of pom.xml modules stays
// rate-limit friendly.
const (
	buildMaxPerKind = 10
	buildMaxPerRepo = 30
)

// Select picks every non-directory entry whose basename is a registered
// build manifest, then applies the per-kind and per-repository caps in
// sorted order — the same repository always yields the same file set no
// matter the order the platform returned the tree in.
func (BuildChannel) Select(tree []FileEntry) []string {
	var found []string
	for _, entry := range tree {
		if entry.IsDir {
			continue
		}
		if _, known := buildParsers[strings.ToLower(basename(entry.Path))]; known {
			found = append(found, entry.Path)
		}
	}
	sort.Strings(found)

	capped := make([]string, 0, buildMaxPerRepo)
	perKind := map[string]int{}
	for _, path := range found {
		kind := strings.ToLower(basename(path))
		if perKind[kind] >= buildMaxPerKind {
			continue
		}
		perKind[kind]++
		capped = append(capped, path)
		if len(capped) >= buildMaxPerRepo {
			break
		}
	}
	return capped
}

// Parse turns one build manifest into channel output: direct (non-managed)
// dependencies become free-text deps and confirmed BUILD evidence; the
// manifest's own identity becomes aliases (composite "g:a" plus the bare
// artifact, so evidence naming either resolves back here). Managed Maven
// entries are a version policy and never become evidence.
func (BuildChannel) Parse(filename, content string) ChannelOutput {
	result, known := parseBuildFile(filename, content)
	if !known {
		return ChannelOutput{}
	}

	output := ChannelOutput{}
	for _, dep := range result.Deps {
		if dep.Managed {
			continue
		}
		output.Deps = append(output.Deps, dep.Coordinates())
		output.Evidence = append(output.Evidence, DepEvidence{
			Name:       dep.Coordinates(),
			Mechanism:  MechanismBuild,
			Confidence: ConfidenceConfirmed,
		})
	}
	output.Identifiers = identityAliases(result.Identity)
	return output
}

// identityAliases returns the aliases a declared identity answers to, most
// precise first: a Maven "com.example:auth-service" yields both the
// composite and the bare artifactId.
func identityAliases(identity string) []string {
	if identity == "" {
		return nil
	}
	group, artifact, found := strings.Cut(identity, ":")
	if found && group != "" && artifact != "" {
		return []string{identity, artifact}
	}
	return []string{identity}
}

func basename(path string) string {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}
