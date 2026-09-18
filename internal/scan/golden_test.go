package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/reposcan"
)

// Golden replay acceptance (design doc §6): the fixtures under
// testdata/golden_scan were frozen from real repositories and the expected
// cards were recorded from the Python implementation. The Go pipeline must
// reproduce those cards field for field. Approved deltas are listed in the
// design doc; anything else that differs is a defect.

type goldenEvidence struct {
	Name       string `json:"name"`
	Mechanism  string `json:"mechanism"`
	Confidence string `json:"confidence"`
}

// goldenCard mirrors the Python-side card JSON (snake_case keys).
type goldenCard struct {
	TopDirs          []string         `json:"top_dirs"`
	Deps             []string         `json:"deps"`
	DepEvidence      []goldenEvidence `json:"dep_evidence"`
	Identities       []string         `json:"identities"`
	DeployIdentities []string         `json:"deploy_identities"`
	RecentCommits    []string         `json:"recent_commits"`
	ExposedAPIs      []string         `json:"exposed_apis"`
	LowSignal        bool             `json:"low_signal"`
}

// replayFetcher serves a frozen input fixture as a Fetcher.
type replayFetcher struct {
	fixture struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Tree     []reposcan.TreeEntry
		Commits  []string
		Contents map[string]string
	}
}

func (f *replayFetcher) FetchTree(ctx context.Context, repoURL string) ([]reposcan.TreeEntry, error) {
	return append([]reposcan.TreeEntry(nil), f.fixture.Tree...), nil
}

func (f *replayFetcher) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	if limit > len(f.fixture.Commits) {
		limit = len(f.fixture.Commits)
	}
	return append([]string(nil), f.fixture.Commits[:limit]...), nil
}

func (f *replayFetcher) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	if content, ok := f.fixture.Contents[path]; ok && content != "" {
		return content, nil
	}
	return "", fmt.Errorf("golden: %s not available", path)
}

func (f *replayFetcher) FetchHead(ctx context.Context, repoURL string) (string, error) {
	return "golden-head", nil
}

func (f *replayFetcher) ResolveName(ctx context.Context, repoURL string) (string, error) {
	return f.fixture.Name, nil
}

func (f *replayFetcher) ListRepos(ctx context.Context, groupURL string) ([]reposcan.RepoInfo, error) {
	return nil, nil
}

func loadGoldenCard(path string) (goldenCard, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenCard{}, err
	}
	var card goldenCard
	if err := json.Unmarshal(data, &card); err != nil {
		return goldenCard{}, err
	}
	// Normalize nil slices to empty so comparisons are stable.
	normalize := func(slice *[]string) {
		if *slice == nil {
			*slice = []string{}
		}
	}
	normalize(&card.TopDirs)
	normalize(&card.Deps)
	normalize(&card.Identities)
	normalize(&card.DeployIdentities)
	normalize(&card.RecentCommits)
	normalize(&card.ExposedAPIs)
	if card.DepEvidence == nil {
		card.DepEvidence = []goldenEvidence{}
	}
	return card, nil
}

func asGoldenCard(card AutoCard) goldenCard {
	out := goldenCard{
		TopDirs:          card.TopDirs,
		Deps:             card.Deps,
		DepEvidence:      make([]goldenEvidence, 0, len(card.DepEvidence)),
		Identities:       card.Identities,
		DeployIdentities: card.DeployIdentities,
		RecentCommits:    card.RecentCommits,
		ExposedAPIs:      card.ExposedAPIs,
		LowSignal:        card.LowSignal,
	}
	for _, item := range card.DepEvidence {
		out.DepEvidence = append(out.DepEvidence, goldenEvidence{
			Name:       item.Name,
			Mechanism:  string(item.Mechanism),
			Confidence: string(item.Confidence),
		})
	}
	normalize := func(slice *[]string) {
		if *slice == nil {
			*slice = []string{}
		}
	}
	normalize(&out.TopDirs)
	normalize(&out.Deps)
	normalize(&out.Identities)
	normalize(&out.DeployIdentities)
	normalize(&out.RecentCommits)
	normalize(&out.ExposedAPIs)
	return out
}

func TestGoldenReplay(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("testdata", "golden_scan", "*.input.json"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("golden fixtures missing: %v (%d found)", err, len(entries))
	}
	for _, inputPath := range entries {
		name := strings.TrimSuffix(filepath.Base(inputPath), ".input.json")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Name     string               `json:"name"`
				URL      string               `json:"url"`
				Tree     []reposcan.TreeEntry `json:"tree"`
				Commits  []string             `json:"commits"`
				Contents map[string]string    `json:"contents"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			expected, err := loadGoldenCard(_expectedPath(inputPath))
			if err != nil {
				t.Fatal(err)
			}

			replay := &replayFetcher{}
			replay.fixture.Name = fixture.Name
			replay.fixture.URL = fixture.URL
			replay.fixture.Tree = fixture.Tree
			replay.fixture.Commits = fixture.Commits
			replay.fixture.Contents = fixture.Contents

			runner := &Runner{Fetcher: replay, Channels: DefaultChannels()}
			card, err := runner.scanRepository(context.Background(),
				reposcan.RepoInfo{Name: fixture.Name, URL: fixture.URL}, "golden-head")
			if err != nil {
				t.Fatal(err)
			}

			actual := asGoldenCard(*card.AutoCard)
			if fmt.Sprint(actual) != fmt.Sprint(expected) {
				t.Fatalf("golden mismatch\n actual: %+v\n expected: %+v",
					actual, expected)
			}
		})
	}
}

func _expectedPath(inputPath string) string {
	return strings.TrimSuffix(inputPath, ".input.json") + ".expected.json"
}
