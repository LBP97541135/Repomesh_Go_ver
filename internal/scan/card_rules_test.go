package scan

import (
	"fmt"
	"strings"
	"testing"
)

func TestAPIRouteChannelThreeFrameworks(t *testing.T) {
	python := `
@app.get("/orders/{order_id}")
@router.post("/orders")
def handler(): ...
`
	express := `
app.get("/users", handler);
router.post('/users/:id', handler);
server.put(` + "`users/me`" + `, handler);
`
	gin := `
r.GET("/ping", ping)
router.POST("/orders", create)
engine.DELETE("/orders/:id", remove)
`
	output := APIRouteChannel{}.Parse("routes.py", python)
	output.ExposedAPIs = append(output.ExposedAPIs, APIRouteChannel{}.Parse("routes.ts", express).ExposedAPIs...)
	output.ExposedAPIs = append(output.ExposedAPIs, APIRouteChannel{}.Parse("routes.go", gin).ExposedAPIs...)

	// Known wart, ported faithfully: the express pattern also matches
	// "@app.get(\"…\")" (app + double quotes), so fastapi routes appear
	// twice when written with double quotes. Downstream consumers dedupe.
	want := []string{
		"fastapi:/orders/{order_id}", "fastapi:/orders",
		"express:/orders/{order_id}", "express:/orders",
		"express:/users", "express:/users/:id", "express:users/me",
		"gin:/ping", "gin:/orders", "gin:/orders/:id",
	}
	if fmt.Sprint(output.ExposedAPIs) != fmt.Sprint(want) {
		t.Fatalf("routes = %v, want %v", output.ExposedAPIs, want)
	}
	if len(output.Deps) != 0 || len(output.Evidence) != 0 {
		t.Fatalf("routes are card metadata only: %+v", output)
	}
}

func TestAPIRouteChannelSelectSkipsIgnoredDirsAndCaps(t *testing.T) {
	var tree []FileEntry
	tree = append(tree,
		FileEntry{Path: "app.py", IsDir: false},
		FileEntry{Path: "node_modules/pkg/app.js", IsDir: false},    // ignored dir
		FileEntry{Path: "dist/bundle.js", IsDir: false},             // ignored dir
		FileEntry{Path: "venv/lib/site.py", IsDir: false},           // ignored dir
		FileEntry{Path: "internal/handler/orders.go", IsDir: false}, // fine
	)
	for index := range 35 {
		tree = append(tree, FileEntry{Path: fmt.Sprintf("src/route%02d.py", index), IsDir: false})
	}

	selected := APIRouteChannel{}.Select(tree)
	if len(selected) != 30 {
		t.Fatalf("selected = %d, want capped at 30", len(selected))
	}
	for _, path := range selected {
		if containsIgnoredDir(path) {
			t.Fatalf("%q crosses an ignored directory", path)
		}
	}
	if strings.Contains(strings.Join(selected, ","), "node_modules") {
		t.Fatalf("ignored directories leaked: %v", selected)
	}
}

func TestDedupeScanOutputFourWays(t *testing.T) {
	deps := []string{"Org.Services:ts-common", "org.services:ts-common", "orders-db"}
	evidence := []DepEvidence{
		{Name: "ts-common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		{Name: "ts-common", Mechanism: MechanismSource, Confidence: ConfidenceConfirmed},
		{Name: "ts-common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
	}
	identities := []string{"Orders", "orders"}
	deployIDs := []string{"orders-api"}

	deps, evidence, identities, deployIDs = DedupeScanOutput(deps, evidence, identities, deployIDs)

	if len(deps) != 2 {
		t.Fatalf("deps = %v, want case-insensitive dedupe to 2", deps)
	}
	if len(evidence) != 2 {
		t.Fatalf("evidence = %+v, want 2 (same name, different mechanism kept)", evidence)
	}
	if len(identities) != 1 {
		t.Fatalf("identities = %v", identities)
	}
	if len(deployIDs) != 1 {
		t.Fatalf("deploy identities = %v", deployIDs)
	}
}

func TestComputeLowSignal(t *testing.T) {
	cases := []struct {
		name    string
		repo    string
		dirs    []string
		deps    []string
		commits []string
		want    bool
	}{
		{"everything generic", "misc", []string{"src", "lib"}, []string{"fastapi"}, []string{"wip"}, true},
		{"business name token lifts above threshold", "checkout", []string{"src", "lib"}, []string{"fastapi"}, []string{"wip"}, false},
		{"a single 0.2 signal is not enough", "misc", []string{"src", "lib"}, []string{"fastapi"}, []string{"feat: add refunds"}, true},
		{"two 0.2 signals cross the threshold", "misc", []string{"src", "lib"}, []string{"our-custom-sdk"}, []string{"feat: add refunds"}, false},
		{"single non-generic dep is only 0.2, still low", "misc", []string{"src", "lib"}, []string{"our-custom-sdk"}, []string{"wip"}, true},
		{"non-generic dir lifts", "misc", []string{"src", "checkout"}, []string{"fastapi"}, []string{"wip"}, false},
		{"empty card is low signal", "", nil, nil, nil, true},
	}
	for _, item := range cases {
		if got := ComputeLowSignal(item.repo, item.dirs, item.deps, item.commits); got != item.want {
			t.Fatalf("%s: low_signal = %v, want %v", item.name, got, item.want)
		}
	}
}

func TestDedupeExposedAPIsCapsAtFifty(t *testing.T) {
	var routes []string
	for index := range 60 {
		routes = append(routes, fmt.Sprintf("fastapi:/route%02d", index))
	}
	routes = append(routes, "fastapi:/route00") // duplicate

	deduped := DedupeExposedAPIs(routes)
	if len(deduped) != 50 {
		t.Fatalf("deduped = %d, want capped at 50", len(deduped))
	}
	if deduped[0] != "fastapi:/route00" {
		t.Fatalf("first occurrence must win: %q", deduped[0])
	}
}
