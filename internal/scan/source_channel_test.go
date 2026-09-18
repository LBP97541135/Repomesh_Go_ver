package scan

import (
	"fmt"
	"testing"
)

func TestSourceChannelGitmodules(t *testing.T) {
	content := `
[submodule "libs/ts-common"]
	path = libs/ts-common
	url = https://github.com/repomesh-train-ticket/ts-common.git

[submodule "libs/placeholder"]
	path = libs/placeholder
	url = ${PLACEHOLDER_URL}

[submodule "libs/no-url"]
	path = libs/no-url
`
	output := SourceChannel{}.Parse(".gitmodules", content)

	if len(output.Deps) != 1 || output.Deps[0] != "ts-common" {
		t.Fatalf("deps = %v (placeholder and url-less sections contribute nothing)", output.Deps)
	}
	if len(output.Evidence) != 1 ||
		output.Evidence[0].Mechanism != MechanismSource ||
		output.Evidence[0].Confidence != ConfidenceConfirmed {
		t.Fatalf("evidence = %+v", output.Evidence)
	}
}

func TestSourceChannelGoWorkOnlyOutsidePaths(t *testing.T) {
	content := `
go 1.26

use (
	./cmd
	./api
	../shared-lib
)

replace example.com/internal => ../internal-lib
replace example.com/local => ./local
`
	output := SourceChannel{}.Parse("go.work", content)
	want := []string{"shared-lib", "internal-lib"}
	if fmt.Sprint(output.Deps) != fmt.Sprint(want) {
		t.Fatalf("deps = %v, want %v (in-repo paths are not references)", output.Deps, want)
	}
}

func TestSourceChannelPackageWorkspaces(t *testing.T) {
	inRepo := SourceChannel{}.Parse("package.json", `{"workspaces": ["packages/*", "apps/web"]}`)
	if len(inRepo.Deps) != 0 {
		t.Fatalf("in-repo globs are not references: %v", inRepo.Deps)
	}

	escaping := SourceChannel{}.Parse("package.json", `{"workspaces": ["../shared-ui", "packages/*"]}`)
	if len(escaping.Deps) != 1 || escaping.Deps[0] != "shared-ui" {
		t.Fatalf("deps = %v, want [shared-ui]", escaping.Deps)
	}

	packagesForm := SourceChannel{}.Parse("package.json", `{"workspaces": {"packages": ["../shared-lib"]}}`)
	if len(packagesForm.Deps) != 1 || packagesForm.Deps[0] != "shared-lib" {
		t.Fatalf("deps = %v", packagesForm.Deps)
	}
}

func TestSourceChannelCargoWorkspaceAndPathDeps(t *testing.T) {
	content := `
[workspace]
members = ["crates/core", "../shared-crate"]

[dependencies]
serde = "1.0"
shared-crate = { path = "../shared-crate" }

[workspace.dependencies]
util = { path = "../../org/libs/ts-util" }
`
	output := SourceChannel{}.Parse("Cargo.toml", content)

	// "crates/core" is in-repo; the three escaping paths resolve to their
	// terminal qualifying segments, deduplicated ("shared-crate" appears
	// as member and as path dep, first wins).
	want := []string{"shared-crate", "ts-util"}
	if fmt.Sprint(output.Deps) != fmt.Sprint(want) {
		t.Fatalf("deps = %v, want %v", output.Deps, want)
	}
}

func TestSourceChannelSelectKnownFilesOnly(t *testing.T) {
	tree := []FileEntry{
		{Path: ".gitmodules", IsDir: false},
		{Path: "go.work", IsDir: false},
		{Path: "package.json", IsDir: false},
		{Path: "nested/deep/Cargo.toml", IsDir: false},
		{Path: "go.sum", IsDir: false}, // not a source-ref file
	}
	selected := SourceChannel{}.Select(tree)
	if len(selected) != 4 {
		t.Fatalf("selected = %v", selected)
	}
}

func TestSourceChannelTerminalSegmentRule(t *testing.T) {
	cases := map[string]string{
		"../ts-common":           "ts-common",
		"../shared/*":            "shared",
		"../../org/libs/ts-util": "ts-util",
		"..\\windows\\style":     "style",
		"../${placeholder}/x":    "x",
		"../..":                  "", // nothing but dots: no ref
	}
	for path, want := range cases {
		var refs []string
		seen := map[string]bool{}
		addOutsideRef(&refs, seen, path)
		if want == "" {
			if len(refs) != 0 {
				t.Fatalf("path %q produced %v, want none", path, refs)
			}
			continue
		}
		if len(refs) != 1 || refs[0] != want {
			t.Fatalf("path %q produced %v, want [%s]", path, refs, want)
		}
	}
}
