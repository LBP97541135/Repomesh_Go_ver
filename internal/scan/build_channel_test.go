package scan

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePomIdentityAliasesAndManagedExcluded(t *testing.T) {
	content := `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>com.demo</groupId>
  <artifactId>kitchen-sink</artifactId>
  <dependencies>
    <dependency>
      <groupId>org.services</groupId>
      <artifactId>ts-common</artifactId>
      <version>1.0</version>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
    </dependency>
  </dependencies>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.services</groupId>
        <artifactId>ts-managed</artifactId>
        <version>2.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`

	output := BuildChannel{}.Parse("pom.xml", content)

	// The dependencyManagement entry is a version policy, never evidence.
	if len(output.Evidence) != 2 {
		t.Fatalf("evidence = %+v, want 2 (managed excluded)", output.Evidence)
	}
	if output.Evidence[0].Name != "org.services:ts-common" ||
		output.Evidence[0].Mechanism != MechanismBuild ||
		output.Evidence[0].Confidence != ConfidenceConfirmed {
		t.Fatalf("evidence[0] = %+v", output.Evidence[0])
	}
	wantIDs := []string{"com.demo:kitchen-sink", "kitchen-sink"}
	if fmt.Sprint(output.Identifiers) != fmt.Sprint(wantIDs) {
		t.Fatalf("identifiers = %v, want %v", output.Identifiers, wantIDs)
	}
}

func TestParsePomNamespaceAgnostic(t *testing.T) {
	content := `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <artifactId>only-artifact</artifactId>
</project>`
	output := BuildChannel{}.Parse("pom.xml", content)
	if output.Identifiers == nil || output.Identifiers[0] != "only-artifact" {
		t.Fatalf("bare artifactId identity = %v", output.Identifiers)
	}
}

func TestParsePackageJSONSections(t *testing.T) {
	content := `{
	  "name": "@acme/web-app",
	  "dependencies": {"react": "^18.0.0", "lodash": "^4.0.0"},
	  "devDependencies": {"lodash": "^4.2.0", "vitest": "^1.0.0"},
	  "peerDependencies": {"react-dom": "^18.0.0"}
	}`
	output := BuildChannel{}.Parse("package.json", content)

	names := map[string]bool{}
	for _, dep := range output.Deps {
		names[dep] = true
	}
	for _, want := range []string{"react", "lodash", "vitest", "react-dom"} {
		if !names[want] {
			t.Fatalf("dep %q missing from %v", want, output.Deps)
		}
	}
	// Case-insensitive dedupe within the file: lodash appears twice above.
	count := 0
	for _, dep := range output.Deps {
		if strings.EqualFold(dep, "lodash") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("lodash appears %d times, want 1", count)
	}
	if output.Identifiers[0] != "@acme/web-app" {
		t.Fatalf("identity = %v", output.Identifiers)
	}
}

func TestParseGoModBlockAndSingleRequire(t *testing.T) {
	content := `module example.com/orders

go 1.26

require (
	github.com/external/lib v1.2.3
	example.com/orders/api v0.1.0
)

require single.dep/v2 v2.0.0
`
	output := BuildChannel{}.Parse("go.mod", content)

	if output.Identifiers[0] != "example.com/orders" {
		t.Fatalf("identity = %v", output.Identifiers)
	}
	depNames := map[string]bool{}
	for _, dep := range output.Deps {
		depNames[dep] = true
	}
	for _, want := range []string{"github.com/external/lib", "example.com/orders/api", "single.dep/v2"} {
		if !depNames[want] {
			t.Fatalf("dep %q missing from %v", want, output.Deps)
		}
	}
}

func TestParsePyprojectPEP621AndPoetry(t *testing.T) {
	content := `
[project]
name = "warehouse-tools"
dependencies = [
  "requests>=2.31.0",
  "pydantic[email]>=2.0; python_version >= '3.9'",
]

[tool.poetry]
name = "warehouse-tools-poetry"

[tool.poetry.dependencies]
python = "^3.12"
httpx = "^0.27"
`
	output := BuildChannel{}.Parse("pyproject.toml", content)

	depNames := map[string]bool{}
	for _, dep := range output.Deps {
		depNames[strings.ToLower(dep)] = true
	}
	for _, want := range []string{"requests", "pydantic", "httpx"} {
		if !depNames[want] {
			t.Fatalf("dep %q missing from %+v", want, output.Deps)
		}
	}
	for _, gone := range []string{"python_version", "python"} {
		if depNames[gone] {
			t.Fatalf("%q must never become a dependency", gone)
		}
	}
}

func TestParseRequirementsStripsSpecsAndMarkers(t *testing.T) {
	content := `
requests>=2.31.0
pydantic[email]>=2.0 ; python_version >= "3.9"
# comment
-r base.txt
django
`
	output := BuildChannel{}.Parse("requirements.txt", content)

	depNames := []string{}
	for _, dep := range output.Deps {
		depNames = append(depNames, dep)
	}
	if fmt.Sprint(depNames) != fmt.Sprint([]string{"requests", "pydantic", "django"}) {
		t.Fatalf("deps = %v", depNames)
	}
}

func TestParseGradleCoordinatesAndNamedForm(t *testing.T) {
	content := `
dependencies {
    implementation 'org.services:ts-common:1.0'
    testImplementation 'junit:junit:4.13'
    api("org.springframework.boot:spring-boot-starter:3.2.0")
    runtimeOnly group: 'mysql', name: 'mysql-connector-java'
    implementation "${libs.guava}"
}
`
	output := BuildChannel{}.Parse("build.gradle", content)

	// Channel deps are "g:a" coordinates without versions — the same
	// versionless shape the Python original emits.
	got := map[string]bool{}
	for _, coordinate := range output.Deps {
		got[coordinate] = true
	}
	if !got["org.services:ts-common"] {
		t.Fatalf("coordinate form missing: %v", output.Deps)
	}
	if !got["org.springframework.boot:spring-boot-starter"] {
		t.Fatalf("parenthesised form missing: %v", output.Deps)
	}
	if !got["mysql:mysql-connector-java"] {
		t.Fatalf("named form missing: %v", output.Deps)
	}
	if got["junit:junit"] {
		t.Fatalf("test configuration must be excluded: %v", output.Deps)
	}
	if len(output.Deps) != 3 {
		t.Fatalf("placeholder must never match: %v", output.Deps)
	}
}

func TestParseCargoSections(t *testing.T) {
	content := `
[package]
name = "warehouse-ingest"

[dependencies]
serde = "1.0"
local-util = { path = "../local-util" }
shared-thing = { workspace = true }

[build-dependencies]
cc = "1.0"

[dev-dependencies]
criterion = "0.5"
`
	output := BuildChannel{}.Parse("Cargo.toml", content)

	if output.Identifiers[0] != "warehouse-ingest" {
		t.Fatalf("identity = %v", output.Identifiers)
	}
	depNames := map[string]bool{}
	for _, dep := range output.Deps {
		depNames[dep] = true
	}
	for _, want := range []string{"serde", "local-util", "cc"} {
		if !depNames[want] {
			t.Fatalf("dep %q missing from %v", want, output.Deps)
		}
	}
	if depNames["shared-thing"] {
		t.Fatal("workspace-inherit entry declares nothing concrete here")
	}
	if depNames["criterion"] {
		t.Fatal("dev-dependencies are not runtime edges")
	}
}

func TestBuildChannelSelectCapsAndSorting(t *testing.T) {
	var tree []FileEntry
	tree = append(tree, FileEntry{Path: "z/pom.xml", IsDir: false})
	// 12 package.json files: per-kind cap is 10.
	for index := range 12 {
		tree = append(tree, FileEntry{Path: fmt.Sprintf("app%02d/package.json", index), IsDir: false})
	}
	// A file the registry does not know must not be selected.
	tree = append(tree, FileEntry{Path: "setup.py", IsDir: false})
	// Directories are never selected.
	tree = append(tree, FileEntry{Path: "pom.xml", IsDir: true})

	selected := BuildChannel{}.Select(tree)

	if len(selected) != 11 { // 10 package.json + 1 pom.xml (z/ first in sort order? no: sorted, so app00..09 + z/pom.xml)
		t.Fatalf("selected = %d items: %v", len(selected), selected)
	}
	if !strings.HasPrefix(selected[0], "app") {
		t.Fatalf("selection must be sorted: %v", selected)
	}
	if selected[len(selected)-1] != "z/pom.xml" {
		t.Fatalf("z/pom.xml must survive the caps: %v", selected)
	}
	for _, path := range selected {
		if path == "setup.py" {
			t.Fatal("setup.py is deliberately unregistered and must not be selected")
		}
	}
}

func TestBuildChannelUnknownFileIsEmpty(t *testing.T) {
	output := BuildChannel{}.Parse("setup.py", "print('hello')")
	if len(output.Deps) != 0 || len(output.Evidence) != 0 {
		t.Fatalf("unregistered file must yield empty output, got %+v", output)
	}
}

// Parity with the synthetic golden fixture in the Python repo: the same
// kitchen-sink pom.xml must produce exactly the BUILD slice recorded there
// (two evidence entries, composite+bare identity, managed excluded).
func TestBuildChannelMatchesGoldenSyntheticFixture(t *testing.T) {
	content := `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>com.demo</groupId>
  <artifactId>kitchen-sink</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.services</groupId>
      <artifactId>ts-common</artifactId>
      <version>1.0</version>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
    </dependency>
  </dependencies>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.services</groupId>
        <artifactId>ts-managed</artifactId>
        <version>2.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`

	output := BuildChannel{}.Parse("pom.xml", content)

	if len(output.Deps) != 2 ||
		output.Deps[0] != "org.services:ts-common" ||
		output.Deps[1] != "org.springframework.boot:spring-boot-starter-data-jpa" {
		t.Fatalf("deps = %v", output.Deps)
	}
	if fmt.Sprint(output.Identifiers) != fmt.Sprint([]string{"com.demo:kitchen-sink", "kitchen-sink"}) {
		t.Fatalf("identifiers = %v", output.Identifiers)
	}
}
