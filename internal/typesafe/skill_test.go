package typesafe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInstallationPreservesConflictsAndOwnership(t *testing.T) {
	dir := t.TempDir()
	if err := InstallSkill(dir, "run-one"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, installedSkillPath, "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "name: typesafe-ai") {
		t.Fatal("official skill missing")
	}
	if len(SkillHash()) != 64 {
		t.Fatal("no reproducible bundle hash")
	}
	if err := InstallSkill(dir, "run-two"); err == nil {
		t.Fatal("existing skill was overwritten")
	}
	if err := CleanupSkill(dir, "run-two"); err == nil {
		t.Fatal("another run removed the skill")
	}
	if err := CleanupSkill(dir, "run-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, installedSkillPath)); !os.IsNotExist(err) {
		t.Fatal("owned skill remains")
	}
	// The original repository can carry its own skill; it must survive cleanup.
	if err := os.MkdirAll(filepath.Join(dir, installedSkillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, installedSkillPath, "SKILL.md"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSkill(dir, "run-one"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, installedSkillPath, "SKILL.md"))
	if string(data) != "original" {
		t.Fatal("repository skill was changed")
	}
}

func TestSkillCannotWriteThroughRepositorySymlink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".agents")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := InstallSkill(dir, "run-one"); err == nil {
		t.Fatal("installation escaped checkout")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("outside directory changed")
	}
}

func TestBrokerEndpointRejectsCredentialRedirectDestinations(t *testing.T) {
	for _, input := range []string{"http://example.org", "https://user:secret@example.org", "https://example.org/arbitrary", "https://example.org?next=private", "file:///tmp/key"} {
		if _, err := BrokerURL(input); err == nil {
			t.Fatal("unsafe broker endpoint accepted")
		}
	}
	for _, input := range []string{"http://127.0.0.1:8080", "http://[::1]:8080", "https://repomesh.example"} {
		endpoint, err := BrokerURL(input)
		if err != nil || !strings.HasSuffix(endpoint, "/api/typesafe/evaluations") {
			t.Fatal("valid broker URL rejected")
		}
	}
}
