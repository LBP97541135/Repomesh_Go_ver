package decisionchain

import (
	"context"
	"testing"
)

func TestScoreOverlapJaccard(t *testing.T) {
	cases := []struct {
		name       string
		query      []string
		repos      []string
		wantScore  float64
		wantShared []string
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 1, []string{"a", "b"}},
		{"one of three", []string{"a", "c"}, []string{"a", "b"}, 1.0 / 3.0, []string{"a"}},
		{"disjoint", []string{"a"}, []string{"b"}, 0, nil},
		{"empty candidate", []string{"a"}, nil, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shared, score := scoreOverlap(c.query, c.repos)
			if (score != c.wantScore) && (score < c.wantScore-1e-9 || score > c.wantScore+1e-9) {
				t.Fatalf("score = %f, want %f", score, c.wantScore)
			}
			if len(shared) != len(c.wantShared) {
				t.Fatalf("shared = %v, want %v", shared, c.wantShared)
			}
		})
	}
}

func TestSimilarAutoDegradesToStructuralWithoutEmbedding(t *testing.T) {
	store := newFakeStore()
	store.structural = []DecisionNode{testNode("node-1", "evt-1", "repo-a")}
	svc := testService(store, Config{})

	result, err := svc.Similar(context.Background(), SimilarQuery{
		Requirement:     "选择仓库",
		RepositoryNames: []string{"repo-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "structural" || len(result.Hits) != 1 {
		t.Fatalf("auto without embedding = %+v, want structural with 1 hit", result)
	}
}

func TestSemanticSearchRequiresQueryText(t *testing.T) {
	svc := testService(newFakeStore(), Config{})
	if _, err := svc.SemanticSearch(context.Background(), "  ", 5, 0); err != ErrBadArgument {
		t.Fatalf("empty queryText err = %v, want ErrBadArgument", err)
	}
}
