package scan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"repomesh.local/repomesh/internal/reposcan"
)

// newScanID generates the catalog primary key for a newly registered
// repository (16 random bytes, hex-encoded).
func newScanID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("reposcan id generation unavailable: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

// Runner is the scan orchestration: it walks the channel table against a
// fetcher for every repository, applies the incremental fingerprint gate,
// and registers results with the catalog. Failure semantics (design §1.6):
// a failed repository becomes a failed card that never enters the catalog
// as plausible empty output, and one repository's failure never interrupts
// the others.
type Runner struct {
	Fetcher      reposcan.Fetcher
	Channels     []EvidenceChannel
	Store        CatalogStore
	MaxWorkers   int
	IncludeForks bool
	// CommitLimit bounds recent commits per card (default 5).
	CommitLimit int
	// OnProgress receives (done, total, repository name) after each
	// repository finishes or is gate-skipped.
	OnProgress func(done, total int, name string)
}

// ScanSingle scans one repository by URL and registers it.
func (r *Runner) ScanSingle(ctx context.Context, repoURL string) (RegistrationCounts, error) {
	counts := RegistrationCounts{Total: 1}

	name, err := r.resolveName(ctx, repoURL)
	if err != nil {
		return counts, err
	}

	head, err := r.Fetcher.FetchHead(ctx, repoURL)
	if err != nil {
		return counts, err
	}
	if r.unchanged(ctx, name, head) {
		counts.Skipped = 1
		return counts, nil
	}

	card, err := r.scanRepository(ctx, reposcan.RepoInfo{Name: name, URL: repoURL}, head)
	if err != nil {
		return counts, err // a single-repo scan asked for by URL fails loudly
	}
	registered, err := RegisterScanned(ctx, r.Store, []RepositoryCard{card})
	if err != nil {
		return counts, err
	}
	counts.Registered = registered.Registered
	counts.Failed = registered.Failed
	return counts, nil
}

// ScanOrganization scans every repository under a group/org and registers
// the outcome. Concurrency is bounded by MaxWorkers; the incremental gate
// short-circuits unchanged repositories before any file is fetched.
func (r *Runner) ScanOrganization(ctx context.Context, groupURL string) (RegistrationCounts, error) {
	repos, err := r.Fetcher.ListRepos(ctx, groupURL)
	if err != nil {
		return RegistrationCounts{}, err
	}
	var targets []reposcan.RepoInfo
	for _, repo := range repos {
		if !repo.Skippable(r.IncludeForks) {
			targets = append(targets, repo)
		}
	}

	maxWorkers := r.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 5
	}
	semaphore := make(chan struct{}, maxWorkers)

	profiles := make([]RepositoryCard, len(targets))
	gateSkipped := 0
	done := 0
	var progressMu sync.Mutex

	var worker sync.WaitGroup
	for index, info := range targets {
		index, info := index, info
		worker.Add(1)
		go func() {
			defer worker.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			card := r.scanOrgRepository(ctx, info, targets, &gateSkipped)
			profiles[index] = card

			progressMu.Lock()
			done++
			if r.OnProgress != nil {
				r.OnProgress(done, len(targets), info.Name)
			}
			progressMu.Unlock()
		}()
	}
	worker.Wait()

	scanned := make([]RepositoryCard, 0, len(profiles))
	for _, card := range profiles {
		if card.ScanStatus != ScanStatusSkipped {
			scanned = append(scanned, card)
		}
	}

	counts, err := RegisterScanned(ctx, r.Store, scanned)
	if err != nil {
		return counts, err
	}
	counts.Total = len(targets)
	counts.Skipped += gateSkipped
	return counts, nil
}

// scanOrgRepository scans one listed repository; a failure becomes a
// failed card (never registered as plausible empty output).
func (r *Runner) scanOrgRepository(ctx context.Context, info reposcan.RepoInfo, targets []reposcan.RepoInfo, gateSkipped *int) RepositoryCard {
	failed := RepositoryCard{
		Name: info.Name, URL: info.URL, Description: info.Description,
		AutoCard: nil, ScanStatus: ScanStatusFailed,
	}

	head, err := r.Fetcher.FetchHead(ctx, info.URL)
	if err != nil {
		return failed
	}
	if r.unchanged(ctx, info.Name, head) {
		*gateSkipped++
		return RepositoryCard{ // not registered; marker for counts only
			Name: info.Name, URL: info.URL, ScanStatus: ScanStatusSkipped,
		}
	}

	card, err := r.scanRepository(ctx, info, head)
	if err != nil {
		return failed
	}
	return card
}

// unchanged reports whether the stored card already reflects the current
// HEAD: same fingerprint and a real card. Manual registrations (no card,
// no fingerprint) never pass the gate — their first re-scan fills them in.
func (r *Runner) unchanged(ctx context.Context, name, head string) bool {
	existing, err := r.Store.GetByName(ctx, name)
	if err != nil || existing == nil {
		return false
	}
	return existing.Fingerprint != "" && existing.Fingerprint == head && existing.AutoCard != nil
}

func (r *Runner) resolveName(ctx context.Context, repoURL string) (string, error) {
	// Platform resolution is preferred but never load-bearing: when the
	// platform cannot confirm the name, the URL's last segment takes over
	// (Python parity).
	name, _ := r.Fetcher.ResolveName(ctx, repoURL)
	if name == "" {
		segments, ok := reposcan.SplitRepoPath(strings.TrimSuffix(strings.TrimRight(repoURL, "/"), ".git"))
		if !ok || len(segments) == 0 {
			return "", fmt.Errorf("不是可识别的仓库地址：%s", repoURL)
		}
		name = segments[len(segments)-1]
	}
	return name, nil
}

// scanRepository runs the channel table for one repository and assembles
// its card: fetch tree once, let every channel select and parse, aggregate
// with the four-way dedupe, derive languages, score low-signal, stamp the
// fingerprint.
func (r *Runner) scanRepository(ctx context.Context, info reposcan.RepoInfo, head string) (RepositoryCard, error) {
	cached := reposcan.NewCache(r.Fetcher)

	tree, err := cached.FetchTree(ctx, info.URL)
	if err != nil {
		return RepositoryCard{}, err
	}
	entries := make([]FileEntry, 0, len(tree))
	for _, entry := range tree {
		entries = append(entries, FileEntry{Path: entry.Path, IsDir: entry.IsDir})
	}

	commitLimit := r.CommitLimit
	if commitLimit <= 0 {
		commitLimit = 5
	}
	// Commits are optional material: a fetch failure degrades to an empty
	// list instead of failing the card (Python parity).
	commits, err := cached.FetchCommits(ctx, info.URL, commitLimit)
	if err != nil {
		commits = []string{}
	}

	var deps, exposedAPIs []string
	var evidence []DepEvidence
	var identities, deployIdentities []string

	for _, channel := range r.Channels {
		for _, path := range channel.Select(entries) {
			content, err := cached.FetchFileContent(ctx, info.URL, path)
			if err != nil {
				continue // this channel sees nothing here; the scan survives
			}
			output := channel.Parse(basename(path), content)
			deps = append(deps, output.Deps...)
			evidence = append(evidence, output.Evidence...)
			identities = append(identities, output.Identifiers...)
			deployIdentities = append(deployIdentities, output.DeployIdentities...)
			exposedAPIs = append(exposedAPIs, output.ExposedAPIs...)
		}
	}

	deps, evidence, identities, deployIdentities = DedupeScanOutput(deps, evidence, identities, deployIdentities)
	exposedAPIs = DedupeExposedAPIs(exposedAPIs)
	topDirs := extractTopDirs(entries)

	card := AutoCard{
		TopDirs:          topDirs,
		Deps:             deps,
		DepEvidence:      evidence,
		Identities:       identities,
		DeployIdentities: deployIdentities,
		RecentCommits:    commits,
		ExposedAPIs:      exposedAPIs,
		LowSignal:        ComputeLowSignal(info.Name, topDirs, deps, commits),
	}

	return RepositoryCard{
		ID:          newScanID(),
		Name:        info.Name,
		URL:         info.URL,
		Description: info.Description,
		AutoCard:    &card,
		ScanStatus:  ScanStatusOK,
		Fingerprint: head,
		Languages:   languagesFromTree(entries),
	}, nil
}

// extractTopDirs: the first two directory levels of the tree, deduplicated,
// capped at 80 entries (prompt size).
func extractTopDirs(tree []FileEntry) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, entry := range tree {
		if !entry.IsDir {
			continue
		}
		parts := strings.Split(entry.Path, "/")
		for depth := 1; depth <= min(len(parts), 2); depth++ {
			dir := strings.Join(parts[:depth], "/")
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	if len(dirs) > 80 {
		dirs = dirs[:80]
	}
	return dirs
}

// languageByManifest maps build manifests to languages (fixed table order
// makes the derived list deterministic).
var languageByManifest = []struct {
	file     string
	language string
}{
	{"requirements.txt", "python"},
	{"pyproject.toml", "python"},
	{"setup.py", "python"},
	{"package.json", "javascript"},
	{"go.mod", "go"},
	{"Cargo.toml", "rust"},
	{"pom.xml", "java"},
	{"build.gradle", "java"},
	{"build.gradle.kts", "java"},
	{"composer.json", "php"},
	{"Gemfile", "ruby"},
}

// languagesFromTree derives the language list from build manifests present
// anywhere in the tree (design D6: the languages field is scan-filled).
func languagesFromTree(tree []FileEntry) []string {
	present := map[string]bool{}
	for _, entry := range tree {
		if !entry.IsDir {
			present[strings.ToLower(basename(entry.Path))] = true
		}
	}
	var languages []string
	for _, mapping := range languageByManifest {
		if present[mapping.file] {
			languages = append(languages, mapping.language)
		}
	}
	return languages
}
