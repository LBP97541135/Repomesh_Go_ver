package scan

import (
	"regexp"
	"strings"
)

// Card-level rules shared by every channel and the aggregation step:
// the ignore list, the generic/business vocabularies behind low-signal
// scoring, and the four-way dedupe. Ported from the Python scan module —
// one copy here replaces its scattered constants.

// ignoredDirs: directories that are never informative for discovery. The
// API-route selection walks source files and must skip them.
var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "__pycache__": true,
	".venv": true, "venv": true, "dist": true, "build": true,
	".next": true, ".turbo": true, "target": true,
	".idea": true, ".vscode": true,
}

// genericDirNames: directory names with no business semantics.
var genericDirNames = map[string]bool{
	"src": true, "lib": true, "libs": true, "test": true, "tests": true,
	"spec": true, "specs": true, "a": true, "b": true, "c": true,
	"handler": true, "handlers": true, "util": true, "utils": true,
	"common": true, "misc": true, "internal": true, "pkg": true,
	"bin": true, "scripts": true, "config": true, "configs": true,
}

// businessNameKeywords: repository names carrying business semantics.
var businessNameKeywords = map[string]bool{
	"pay": true, "order": true, "user": true, "auth": true, "notify": true,
	"email": true, "front": true, "api": true, "service": true, "admin": true,
	"config": true, "cart": true, "checkout": true, "billing": true,
	"invoice": true, "account": true, "profile": true, "search": true,
	"catalog": true, "inventory": true, "shipping": true, "warehouse": true,
	"report": true, "analytics": true, "dashboard": true, "gateway": true,
	"proxy": true, "worker": true, "scheduler": true, "webhook": true,
}

// genericDeps: pure-framework dependencies that are not business-specific.
var genericDeps = map[string]bool{
	"fastapi": true, "flask": true, "django": true, "starlette": true,
	"express": true, "koa": true, "nestjs": true, "react": true, "vue": true,
	"angular": true, "next": true, "nuxt": true, "redis": true, "celery": true,
	"sqlalchemy": true, "sequelize": true, "mongoose": true, "prisma": true,
	"pytest": true, "jest": true, "vitest": true, "mocha": true, "eslint": true,
	"ruff": true, "black": true, "mypy": true, "typescript": true,
	"pydantic": true, "lodash": true, "axios": true, "requests": true,
	"httpx": true, "aiohttp": true,
}

// Vague commits: a conventional prefix or a single word carries no signal.
var (
	vagueCommitPrefix = regexp.MustCompile(`^\s*(fix|bug|update|refactor|wip|chore|tidy|cleanup)\b`)
	vagueSingleWord   = regexp.MustCompile(`^\s*\w+\s*$`)
)

// LowSignalThreshold: a score below this marks the card low-signal.
const LowSignalThreshold = 0.3

var nameTokenPattern = regexp.MustCompile(`[\w-]+`)

// ComputeLowSignal is the single scoring rule for both scanners: +0.3 when
// the repository name carries a business keyword, +0.3 for a non-generic
// directory, +0.2 for a non-generic dependency, +0.2 for a specific
// commit. A total below the threshold marks the card low-signal — AI
// matching conclusions over it are presented as guesses.
func ComputeLowSignal(repoName string, topDirs, deps, recentCommits []string) bool {
	score := 0.0

	tokens := nameTokenPattern.FindAllString(strings.ToLower(repoName), -1)
	for _, token := range tokens {
		if businessNameKeywords[token] {
			score += 0.3
			break
		}
	}

	for _, dir := range topDirs {
		leaf := dir
		if index := strings.LastIndexByte(dir, '/'); index >= 0 {
			leaf = dir[index+1:]
		}
		leaf = strings.ToLower(leaf)
		if leaf != "" && !genericDirNames[leaf] {
			score += 0.3
			break
		}
	}

	for _, dep := range deps {
		if !genericDeps[strings.ToLower(dep)] {
			score += 0.2
			break
		}
	}

	for _, commit := range recentCommits {
		if isSpecificCommit(commit) {
			score += 0.2
			break
		}
	}

	return score < LowSignalThreshold
}

func isSpecificCommit(message string) bool {
	if vagueCommitPrefix.MatchString(message) {
		return false
	}
	return !vagueSingleWord.MatchString(message)
}

// DedupeStrings order-preserving, case-insensitive, first wins.
func DedupeStrings(items []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, item)
	}
	return unique
}

// DedupeScanOutput collapses the four card collections after aggregation:
// deps by name, evidence by name+mechanism (a bare artifact and its Maven
// composite are different keys — the registry decides what they point to),
// identities and deploy identities by name. All order-preserving, first
// wins, case-insensitive.
func DedupeScanOutput(deps []string, evidence []DepEvidence, identities, deployIdentities []string) ([]string, []DepEvidence, []string, []string) {
	uniqueDeps := DedupeStrings(deps)

	uniqueEvidence := make([]DepEvidence, 0, len(evidence))
	seenEvidence := map[string]bool{}
	for _, item := range evidence {
		key := strings.ToLower(item.Name) + "|" + string(item.Mechanism)
		if seenEvidence[key] {
			continue
		}
		seenEvidence[key] = true
		uniqueEvidence = append(uniqueEvidence, item)
	}

	uniqueIdentities := DedupeStrings(identities)
	uniqueDeployIdentities := DedupeStrings(deployIdentities)
	return uniqueDeps, uniqueEvidence, uniqueIdentities, uniqueDeployIdentities
}

// ExposedAPIMaxRoutes caps deduplicated routes per repository.
const ExposedAPIMaxRoutes = 50

// DedupeExposedAPIs order-preserving dedupe + the 50-route cap.
func DedupeExposedAPIs(routes []string) []string {
	unique := DedupeStrings(routes)
	if len(unique) > ExposedAPIMaxRoutes {
		unique = unique[:ExposedAPIMaxRoutes]
	}
	return unique
}
