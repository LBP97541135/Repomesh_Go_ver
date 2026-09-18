package scan

// Build-manifest parsers (mechanism ① BUILD) — the Go port of the Python
// implementation's dep_parsers module. Every parser returns the same shape
// (BuildFileResult): the identity the repository declares for itself, its
// direct build dependencies, and Maven dependencyManagement entries (a
// version policy, never evidence).
//
// Defensive contract, identical to the Python original: malformed content
// yields an empty result, never an error — a scan must survive any one
// unparseable file.
//
// Dispatch is registry-driven (design §3.3): a new ecosystem means
// registering a filename parser in buildParsers, not editing the channel.

import (
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// BuildDep is one build-time dependency with Maven-style coordinates.
type BuildDep struct {
	Name    string
	Version string
	GroupID string
	Managed bool
}

// Coordinates is the identifier other repositories use to reference this dep.
func (d BuildDep) Coordinates() string {
	if d.GroupID != "" {
		return d.GroupID + ":" + d.Name
	}
	return d.Name
}

// BuildFileResult is what one build manifest declares about itself.
type BuildFileResult struct {
	Identity string
	Deps     []BuildDep
	Managed  []BuildDep
}

var buildFileResultEmpty = BuildFileResult{}

// buildParsers is the registry: build-manifest filename → parser.
// setup.py is deliberately absent — parsing it would require executing or
// AST-parsing Python code for a marginal legacy ecosystem, so it is not
// claimed as supported.
var buildParsers = map[string]func(string) BuildFileResult{
	"pom.xml":          parsePom,
	"package.json":     parsePackageJSON,
	"go.mod":           parseGoMod,
	"pyproject.toml":   parsePyproject,
	"requirements.txt": parseRequirements,
	"build.gradle":     parseGradle,
	"build.gradle.kts": parseGradle,
	"cargo.toml":       parseCargo, // lookup lowercases the basename, so Cargo.toml lands here
}

// parseBuildFile dispatches on the manifest's basename (nested build files
// resolve too). A filename outside the registry yields nil rather than a
// guess.
func parseBuildFile(filename, content string) (BuildFileResult, bool) {
	parser, known := buildParsers[strings.ToLower(filename)]
	if !known {
		return BuildFileResult{}, false
	}
	return parser(content), true
}

// ---------------------------------------------------------------------------
// pom.xml — encoding/xml, namespace-agnostic
// ---------------------------------------------------------------------------

type pomNode struct {
	XMLName  xml.Name
	Children []pomNode `xml:",any"`
	Text     string    `xml:",chardata"`
}

func (n pomNode) child(name string) *pomNode {
	for index := range n.Children {
		if localName(n.Children[index].XMLName.Local) == name {
			return &n.Children[index]
		}
	}
	return nil
}

func (n pomNode) children(name string) []*pomNode {
	var found []*pomNode
	for index := range n.Children {
		if localName(n.Children[index].XMLName.Local) == name {
			found = append(found, &n.Children[index])
		}
	}
	return found
}

func localName(tag string) string {
	if index := strings.LastIndexByte(tag, '}'); index >= 0 {
		return tag[index+1:]
	}
	return tag
}

func pomText(node *pomNode) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Text)
}

func parsePom(content string) BuildFileResult {
	var root pomNode
	if err := xml.Unmarshal([]byte(content), &root); err != nil {
		return buildFileResultEmpty
	}

	groupID := pomText(root.child("groupId"))
	artifactID := pomText(root.child("artifactId"))
	identity := ""
	switch {
	case artifactID != "" && groupID != "":
		identity = groupID + ":" + artifactID
	case artifactID != "":
		identity = artifactID
	}

	result := BuildFileResult{Identity: identity}
	if dependencies := root.child("dependencies"); dependencies != nil {
		for _, dep := range dependencies.children("dependency") {
			result.Deps = append(result.Deps, parsePomDependency(*dep, false))
		}
	}
	if management := root.child("dependencyManagement"); management != nil {
		if dependencies := management.child("dependencies"); dependencies != nil {
			for _, dep := range dependencies.children("dependency") {
				result.Managed = append(result.Managed, parsePomDependency(*dep, true))
			}
		}
	}
	return result
}

func parsePomDependency(node pomNode, managed bool) BuildDep {
	return BuildDep{
		Name:    pomText(node.child("artifactId")),
		Version: pomText(node.child("version")),
		GroupID: pomText(node.child("groupId")),
		Managed: managed,
	}
}

// ---------------------------------------------------------------------------
// package.json
// ---------------------------------------------------------------------------

// parsePackageJSON reads name identity plus the three dependency sections,
// preserving the manifest's key order — Python's json.loads kept insertion
// order and the card's dep sequence is compared against the golden fixtures.
func parsePackageJSON(content string) BuildFileResult {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return buildFileResultEmpty
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return buildFileResultEmpty
	}

	result := BuildFileResult{}
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return buildFileResultEmpty
		}
		key, _ := keyToken.(string)
		switch key {
		case "name":
			var name string
			if err := decoder.Decode(&name); err == nil && strings.TrimSpace(name) != "" {
				result.Identity = name
			}
		case "dependencies", "devDependencies", "peerDependencies":
			if err := decodePackageSection(decoder, &result, seen); err != nil {
				return buildFileResultEmpty
			}
		default:
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return buildFileResultEmpty
			}
		}
	}
	return result
}

func decodePackageSection(decoder *json.Decoder, result *BuildFileResult, seen map[string]bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil // a non-object section contributes nothing
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, _ := keyToken.(string)
		var version json.RawMessage
		if err := decoder.Decode(&version); err != nil {
			return err
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		text := strings.TrimSpace(string(version))
		result.Deps = append(result.Deps, BuildDep{Name: name, Version: strings.Trim(text, `"'`)})
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// go.mod
// ---------------------------------------------------------------------------

func parseGoMod(content string) BuildFileResult {
	result := BuildFileResult{}
	seen := map[string]bool{}
	inRequireBlock := false

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "module ") && result.Identity == "":
			result.Identity = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case strings.HasPrefix(line, "require ("):
			inRequireBlock = true
		case inRequireBlock && line == ")":
			inRequireBlock = false
		case inRequireBlock && line != "" && !strings.HasPrefix(line, "//"):
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				addGoDep(&result, seen, parts[0])
			}
		case strings.HasPrefix(line, "require ") && !strings.Contains(line, "("):
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addGoDep(&result, seen, parts[1])
			}
		}
	}
	return result
}

func addGoDep(result *BuildFileResult, seen map[string]bool, path string) {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "//") {
		return
	}
	key := strings.ToLower(path)
	if seen[key] {
		return
	}
	seen[key] = true
	result.Deps = append(result.Deps, BuildDep{Name: path})
}

// ---------------------------------------------------------------------------
// pyproject.toml — PEP 621 [project] and the Poetry dialect
// ---------------------------------------------------------------------------

func parsePyproject(content string) BuildFileResult {
	var data map[string]any
	if err := toml.Unmarshal([]byte(content), &data); err != nil {
		return buildFileResultEmpty
	}

	result := BuildFileResult{}
	seen := map[string]bool{}
	if project, ok := data["project"].(map[string]any); ok {
		result.Identity = tomlString(project["name"])
		for _, dep := range pep508List(project["dependencies"]) {
			appendPyDepWith(&result, seen, dep.Name, dep.Version)
		}
	}
	if tool, ok := data["tool"].(map[string]any); ok {
		if poetry, ok := tool["poetry"].(map[string]any); ok {
			if result.Identity == "" {
				result.Identity = tomlString(poetry["name"])
			}
			if table, ok := poetry["dependencies"].(map[string]any); ok {
				for name, spec := range table {
					if name == "python" {
						continue // the interpreter constraint is not a dependency
					}
					appendPyDepWith(&result, seen, name, poetryVersion(spec))
				}
			}
		}
	}
	return result
}

func parseRequirements(content string) BuildFileResult {
	result := BuildFileResult{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		name, version := splitPEP508(line)
		appendPyDepWith(&result, seen, name, version)
	}
	return result
}

var pep508Name = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)`)

// splitPEP508 splits one requirement into (name, specifier): extras in
// brackets and environment markers after ";" are dropped, per the Python
// original.
func splitPEP508(item string) (string, string) {
	body := strings.TrimSpace(strings.Split(item, ";")[0])
	match := pep508Name.FindStringSubmatch(body)
	if match == nil {
		return body, ""
	}
	return match[1], strings.TrimSpace(body[len(match[1]):])
}

func pep508List(value any) []BuildDep {
	var deps []BuildDep
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				name, version := splitPEP508(text)
				deps = append(deps, BuildDep{Name: name, Version: version})
			}
		}
	case map[string]any:
		for name, spec := range typed {
			deps = append(deps, BuildDep{Name: name, Version: poetryVersion(spec)})
		}
	}
	return deps
}

func tomlString(value any) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return ""
}

func poetryVersion(spec any) string {
	if text, ok := spec.(string); ok {
		return text
	}
	if table, ok := spec.(map[string]any); ok {
		return tomlString(table["version"])
	}
	return ""
}

func appendPyDepWith(result *BuildFileResult, seen map[string]bool, name, version string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if seen[key] {
		return
	}
	seen[key] = true
	result.Deps = append(result.Deps, BuildDep{Name: name, Version: version})
}

// ---------------------------------------------------------------------------
// build.gradle / build.gradle.kts — Groovy & Kotlin DSL coordinate subset
// ---------------------------------------------------------------------------

// gradleIgnoredConfigPrefixes: test-only configurations are excluded — a
// test dependency is not a runtime edge.
var gradleIgnoredConfigPrefixes = []string{"test", "androidtest", "testfixtures"}

var (
	// gradleCoord matches `config 'g:a:v'` and `config('g:a:v')`; `$` never
	// appears in a concrete coordinate, so ${libs.foo} placeholders are
	// excluded by construction.
	gradleCoord = regexp.MustCompile(`(\w+)\s*\(?\s*['"]([^'"$]+:[^'"$]+:[^'"$]+)['"]`)
	// gradleNamed matches `config group: 'g', name: 'n'`.
	gradleNamed = regexp.MustCompile(`(\w+)\s*(?:\(\s*)?group\s*:\s*['"]([^'"]+)['"]\s*,\s*name\s*:\s*['"]([^'"]+)['"]`)
	// gradleBlock extracts dependencies { ... } blocks. Nested blocks are not
	// supported — the same limitation the Python original shipped with.
	gradleBlock = regexp.MustCompile(`(?s)dependencies\s*\{([^}]*)\}`)
)

func parseGradle(content string) BuildFileResult {
	result := BuildFileResult{}
	seen := map[string]bool{}

	for _, block := range gradleBlock.FindAllStringSubmatch(content, -1) {
		for _, match := range gradleCoord.FindAllStringSubmatch(block[1], -1) {
			config, coordinates := match[1], match[2]
			if isTestConfig(config) {
				continue
			}
			parts := strings.Split(coordinates, ":")
			groupID := parts[0]
			name := coordinates
			version := ""
			if len(parts) >= 2 {
				name = parts[1]
			}
			if len(parts) >= 3 {
				version = parts[2]
			}
			addGradleDep(&result, seen, groupID, name, version)
		}
		for _, match := range gradleNamed.FindAllStringSubmatch(block[1], -1) {
			config, groupID, name := match[1], match[2], match[3]
			if isTestConfig(config) {
				continue
			}
			addGradleDep(&result, seen, groupID, name, "")
		}
	}
	return result
}

func isTestConfig(config string) bool {
	lowered := strings.ToLower(config)
	for _, prefix := range gradleIgnoredConfigPrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

func addGradleDep(result *BuildFileResult, seen map[string]bool, groupID, name, version string) {
	key := strings.ToLower(groupID + ":" + name)
	if seen[key] {
		return
	}
	seen[key] = true
	result.Deps = append(result.Deps, BuildDep{Name: name, Version: version, GroupID: groupID})
}

// ---------------------------------------------------------------------------
// Cargo.toml
// ---------------------------------------------------------------------------

func parseCargo(content string) BuildFileResult {
	var data map[string]any
	if err := toml.Unmarshal([]byte(content), &data); err != nil {
		return buildFileResultEmpty
	}

	identity := ""
	if pkg, ok := data["package"].(map[string]any); ok {
		identity = tomlString(pkg["name"])
	}

	result := BuildFileResult{Identity: identity}
	seen := map[string]bool{}
	// [dev-dependencies] are excluded (a test-only crate is not a runtime
	// edge); [workspace.dependencies] is a shared version policy, never a
	// direct dependency of this crate.
	for _, section := range []string{"dependencies", "build-dependencies"} {
		table, ok := data[section].(map[string]any)
		if !ok {
			continue
		}
		for name, spec := range table {
			if cargoWorkspaceInheritOnly(spec) {
				continue // { workspace = true } inherits, declares nothing here
			}
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Deps = append(result.Deps, BuildDep{Name: name, Version: cargoVersion(spec)})
		}
	}
	return result
}

// cargoWorkspaceInheritOnly reports whether the spec inherits everything
// from [workspace.dependencies] and declares no concrete coordinate here.
func cargoWorkspaceInheritOnly(spec any) bool {
	table, ok := spec.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"version", "path", "git"} {
		if _, concrete := table[key]; concrete {
			return false
		}
	}
	return true
}

func cargoVersion(spec any) string {
	if text, ok := spec.(string); ok {
		return text
	}
	if table, ok := spec.(map[string]any); ok {
		return tomlString(table["version"])
	}
	return ""
}
