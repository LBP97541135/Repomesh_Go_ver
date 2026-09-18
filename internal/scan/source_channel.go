package scan

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SourceChannel is mechanism ⑤ SOURCE: another repository's code pulled in
// without a build artifact — submodule URLs, go.work use/replace paths,
// workspace globs and Cargo path dependencies. The rule is deliberately
// strict: only references that cross the repository boundary count. These
// are executed by the compiler/package manager, so the evidence is
// confirmed.
type SourceChannel struct{}

// NewSourceChannel returns the channel ready for RegisterChannel.
func NewSourceChannel() SourceChannel { return SourceChannel{} }

// Name is the channel id and the dedupe namespace for its evidence.
func (SourceChannel) Name() string { return "SOURCE" }

// sourceRefFileNames: the files that may pin cross-repository code. The
// BUILD channel also reads package.json/Cargo.toml — each mechanism keeps
// its own parse of the content (a small file, and the channels stay
// independent).
var sourceRefFileNames = map[string]bool{
	".gitmodules":  true,
	"go.work":      true,
	"package.json": true,
	"Cargo.toml":   true,
}

// sourceRefMaxFiles bounds upstream fetches per repository.
const sourceRefMaxFiles = 10

// Select picks source-reference files anywhere in the tree, sorted.
func (SourceChannel) Select(tree []FileEntry) []string {
	var found []string
	for _, entry := range tree {
		if !entry.IsDir && sourceRefFileNames[basename(entry.Path)] {
			found = append(found, entry.Path)
		}
	}
	sort.Strings(found)
	if len(found) > sourceRefMaxFiles {
		found = found[:sourceRefMaxFiles]
	}
	return found
}

// Parse dispatches by basename; anything else contributes nothing. Never
// fails: malformed content yields empty output.
func (SourceChannel) Parse(filename, content string) ChannelOutput {
	output := ChannelOutput{}
	for _, name := range parseSourceRefFile(basename(filename), content) {
		output.Deps = append(output.Deps, name)
		output.Evidence = append(output.Evidence, DepEvidence{
			Name:       name,
			Mechanism:  MechanismSource,
			Confidence: ConfidenceConfirmed,
		})
	}
	return output
}

// ---------------------------------------------------------------------------
// per-file parsers
// ---------------------------------------------------------------------------

// parseSourceRefFile dispatches on the basename.
func parseSourceRefFile(base, content string) []string {
	switch base {
	case ".gitmodules":
		return parseGitmodules(content)
	case "go.work":
		return parseGoWork(content)
	case "package.json":
		return parsePackageWorkspaces(content)
	case "Cargo.toml":
		return parseCargoWorkspace(content)
	}
	return nil
}

var submoduleHeader = regexp.MustCompile(`^\[submodule\s+"([^"]+)"\]\s*$`)

// gitURLTail matches the terminal segment of https/ssh/scp-style URLs:
// "https://host/owner/repo.git", "git@host:owner/repo.git" and
// "ssh://git@host/owner/repo" all yield "repo(.git)".
var gitURLTail = regexp.MustCompile(`[^/:@]+(?:\.git)?$`)

// parseGitmodules extracts submodule repo names. Each [submodule "…"]
// section may declare url = …; the URL's terminal segment (sans .git) is
// the referenced repository's name. Sections without a URL — or with a
// ${…} placeholder — contribute nothing.
func parseGitmodules(content string) []string {
	var refs []string
	seen := map[string]bool{}
	currentURL := ""
	inSection := false

	flush := func() {
		if currentURL != "" {
			appendSourceRef(&refs, seen, repoNameFromGitURL(currentURL))
		}
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if submoduleHeader.MatchString(line) {
			flush()
			currentURL = ""
			inSection = true
			continue
		}
		// A url line only counts inside a [submodule "…"] section.
		if inSection && strings.HasPrefix(strings.ToLower(line), "url ") {
			if index := strings.IndexAny(line, " \t"); index >= 0 {
				candidate := strings.TrimSpace(line[index+1:])
				currentURL = strings.Trim(candidate, `"`)
			}
		}
	}
	flush()
	return refs
}

// repoNameFromGitURL: "https://host/owner/repo.git" → "repo". A ${…}
// placeholder yields "" (skipped by the caller).
func repoNameFromGitURL(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" || strings.Contains(value, "$") {
		return ""
	}
	match := gitURLTail.FindString(value)
	if match == "" {
		return ""
	}
	return strings.TrimSuffix(match, ".git")
}

var goWorkDirToken = regexp.MustCompile(`[./\w-]+`)

// parseGoWork extracts cross-boundary use/replace paths from a go.work.
// Only paths starting with ".." escape this repository's root — in-repo
// "use ./cmd" contributes nothing.
func parseGoWork(content string) []string {
	var refs []string
	seen := map[string]bool{}
	inUseBlock, inReplaceBlock := false, false

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "use (":
			inUseBlock = true
		case inUseBlock && line == ")":
			inUseBlock = false
		case inUseBlock:
			addGoUse(&refs, seen, line)
		case line == "replace (":
			inReplaceBlock = true
		case inReplaceBlock && line == ")":
			inReplaceBlock = false
		case inReplaceBlock:
			addGoReplace(&refs, seen, line)
		case strings.HasPrefix(line, "use "):
			for _, token := range strings.Fields(line)[1:] {
				addGoUse(&refs, seen, token)
			}
		case strings.HasPrefix(line, "replace "):
			addGoReplace(&refs, seen, line)
		}
	}
	return refs
}

func addGoUse(refs *[]string, seen map[string]bool, line string) {
	for _, token := range goWorkDirToken.FindAllString(line, -1) {
		if strings.HasPrefix(token, "..") {
			addOutsideRef(refs, seen, token)
		}
	}
}

// addGoReplace: a "replace old => path" entry — only the local target counts.
func addGoReplace(refs *[]string, seen map[string]bool, line string) {
	index := strings.Index(line, "=>")
	if index < 0 {
		return
	}
	target := strings.TrimSpace(line[index+2:])
	if strings.HasPrefix(target, "..") {
		addOutsideRef(refs, seen, target)
	}
}

// parsePackageWorkspaces extracts cross-boundary workspace globs:
// "workspaces" may be a list of globs or {"packages": […]}. "packages/*"
// is this repo's own layout and contributes nothing.
func parsePackageWorkspaces(content string) []string {
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil
	}
	var patterns []string
	switch typed := data["workspaces"].(type) {
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				patterns = append(patterns, text)
			}
		}
	case map[string]any:
		if packages, ok := typed["packages"].([]any); ok {
			for _, item := range packages {
				if text, ok := item.(string); ok {
					patterns = append(patterns, text)
				}
			}
		}
	}

	var refs []string
	seen := map[string]bool{}
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		if strings.HasPrefix(trimmed, "..") {
			addOutsideRef(&refs, seen, trimmed)
		}
	}
	return refs
}

// parseCargoWorkspace: [workspace].members entries that escape the root
// ("../shared-crate") and path = "../…" values in the dependency tables.
// In-workspace members ("crates/*") and registry deps are not references.
func parseCargoWorkspace(content string) []string {
	var data map[string]any
	if err := toml.Unmarshal([]byte(content), &data); err != nil {
		return nil
	}

	var refs []string
	seen := map[string]bool{}

	workspace, _ := data["workspace"].(map[string]any)
	if workspace != nil {
		if members, ok := workspace["members"].([]any); ok {
			for _, member := range members {
				if text, ok := member.(string); ok && strings.HasPrefix(strings.TrimSpace(text), "..") {
					addOutsideRef(&refs, seen, strings.TrimSpace(text))
				}
			}
		}
	}
	for _, section := range []string{"dependencies", "dev-dependencies"} {
		if table, ok := data[section].(map[string]any); ok {
			addCargoPathDeps(&refs, seen, table)
		}
	}
	if workspace != nil {
		if wdeps, ok := workspace["dependencies"].(map[string]any); ok {
			addCargoPathDeps(&refs, seen, wdeps)
		}
	}
	return refs
}

func addCargoPathDeps(refs *[]string, seen map[string]bool, table map[string]any) {
	for _, spec := range table {
		entry, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		if path, ok := entry["path"].(string); ok && strings.HasPrefix(strings.TrimSpace(path), "..") {
			addOutsideRef(refs, seen, strings.TrimSpace(path))
		}
	}
}

// addOutsideRef appends a ref for a "../" path — the terminal qualifying
// segment wins: "../ts-common" → "ts-common"; "../shared/*" → "shared";
// "../../org/libs/ts-util" → "ts-util". Placeholders and wildcard segments
// are skipped; first seen wins, case-insensitively.
func addOutsideRef(refs *[]string, seen map[string]bool, path string) {
	normalized := strings.ReplaceAll(path, "\\", "/")
	segments := strings.Split(normalized, "/")
	for index := len(segments) - 1; index >= 0; index-- {
		segment := segments[index]
		if segment == "." || segment == ".." || strings.Contains(segment, "*") {
			continue
		}
		if strings.TrimSpace(segment) != "" && !strings.Contains(segment, "$") {
			appendSourceRef(refs, seen, segment)
		}
		return
	}
}

func appendSourceRef(refs *[]string, seen map[string]bool, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if seen[key] {
		return
	}
	seen[key] = true
	*refs = append(*refs, name)
}
