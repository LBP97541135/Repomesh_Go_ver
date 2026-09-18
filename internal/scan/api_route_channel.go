package scan

import (
	"regexp"
	"sort"
	"strings"
)

// APIRouteChannel collects exposed HTTP routes (fastapi/express/gin
// shapes) as card metadata for prompt clarity. Routes are never evidence —
// nothing enters deps or dep_evidence here.
type APIRouteChannel struct{}

// NewAPIRouteChannel returns the channel ready for RegisterChannel.
func NewAPIRouteChannel() APIRouteChannel { return APIRouteChannel{} }

// Name is the channel id. This channel contributes no evidence, so the
// namespace is only organisational.
func (APIRouteChannel) Name() string { return "API_ROUTES" }

// apiSourceSuffixes: the same source set as the Python scanners, so local
// and remote extraction stay structurally identical.
var apiSourceSuffixes = []string{".py", ".ts", ".tsx", ".js", ".jsx", ".go"}

// apiRouteMaxFiles: source files fetched per repository.
const apiRouteMaxFiles = 30

// Select picks source files likely to declare API routes, skipping ignored
// directories, sorted, capped.
func (APIRouteChannel) Select(tree []FileEntry) []string {
	var candidates []string
	for _, entry := range tree {
		if entry.IsDir {
			continue
		}
		if !hasAnySuffix(entry.Path, apiSourceSuffixes) {
			continue
		}
		if containsIgnoredDir(entry.Path) {
			continue
		}
		candidates = append(candidates, entry.Path)
	}
	sort.Strings(candidates)
	if len(candidates) > apiRouteMaxFiles {
		candidates = candidates[:apiRouteMaxFiles]
	}
	return candidates
}

func hasAnySuffix(value string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func containsIgnoredDir(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if ignoredDirs[segment] {
			return true
		}
	}
	return false
}

// One capture group each: the route path. The framework prefix is added by
// the matcher so the card reads "fastapi:/orders".
var apiRoutePatterns = []struct {
	framework string
	pattern   *regexp.Regexp
}{
	{"fastapi", regexp.MustCompile(`(?i)@\w+\.(?:get|post|put|delete|patch|head|options)\(\s*"([^"]+)"`)},
	// Express accepts ', " and ` quotes around the path.
	{"express", regexp.MustCompile("\\b(?:app|router|server)\\.(?:get|post|put|delete|patch)\\(\\s*[\"'`]([^\"'`]+)[\"'`]")},
	{"gin", regexp.MustCompile(`\b(?:r|router|engine|group)\.(?:GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]+)"`)},
}

// Parse matches routes in one source file. Output goes to ExposedAPIs
// only — deps and evidence stay untouched.
func (APIRouteChannel) Parse(filename, content string) ChannelOutput {
	output := ChannelOutput{}
	for _, pattern := range apiRoutePatterns {
		for _, match := range pattern.pattern.FindAllStringSubmatch(content, -1) {
			if route := strings.TrimSpace(match[1]); route != "" {
				output.ExposedAPIs = append(output.ExposedAPIs, pattern.framework+":"+route)
			}
		}
	}
	return output
}
