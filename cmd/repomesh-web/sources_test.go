package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/sources"
	"repomesh.local/repomesh/internal/testdb"
)

func TestSourcesCLIImportWithoutAuthRuntime(t *testing.T) {
	pool, databaseURL := testdb.OpenWithURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ('cli-owner-1',9301,'CLI owner')`); err != nil {
		t.Fatal(err)
	}
	const importID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	manifest := `{
		"schemaVersion":1,
		"importId":"` + importID + `",
		"environmentTemplates":[{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}],
		"executionProfiles":[{"id":"exec","version":"v1","ownerId":"cli-owner-1","name":"执行","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false}],
		"defaultBindings":[{"kind":"execution","ownerId":"cli-owner-1","profileId":"exec","profileVersion":"v1"}]
	}`
	directory := t.TempDir()
	path := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPOMESH_DATABASE_URL", databaseURL)
	t.Setenv("REPOMESH_AUTH_CONFIG", filepath.Join(directory, "missing-auth.json"))
	t.Setenv("REPOMESH_WEB_ASSETS", "")
	var stdout, stderr bytes.Buffer
	if code := run(ctx, []string{"sources", "import", "--file", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, stderr.String())
	}
	var receipt sources.Receipt
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil || receipt.ImportID != importID || receipt.SchemaVersion != 1 || len(receipt.DefaultBindings) != 1 {
		t.Fatalf("import stdout=%s", stdout.String())
	}
	if receipt.DefaultBindings[0].OwnerID != "cli-owner-1" || receipt.DefaultBindings[0].ProfileID != "exec" {
		t.Fatalf("import receipt=%+v", receipt)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(ctx, []string{"sources", "result", "--import-id", importID}, &stdout, &stderr); code != 0 {
		t.Fatalf("result exit=%d stderr=%s", code, stderr.String())
	}
	var lookup sources.Receipt
	if err := json.Unmarshal(stdout.Bytes(), &lookup); err != nil || lookup.ImportID != receipt.ImportID || !lookup.CommittedAt.Equal(receipt.CommittedAt) {
		t.Fatalf("result stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "missing-auth.json") {
		t.Fatal("sources CLI opened auth runtime")
	}
}
