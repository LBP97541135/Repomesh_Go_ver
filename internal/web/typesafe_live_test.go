package web

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
	"repomesh.local/repomesh/internal/typesafe"
)

// Opt in explicitly. This sends only synthetic evidence and runs Codex with a
// scoped grant. Neither the provider key nor its file path reaches Codex.
func TestTypeSafeLiveCodexSkill(t *testing.T) {
	keyPath := os.Getenv("REPOMESH_TYPESAFE_LIVE_KEY_FILE")
	if keyPath == "" {
		t.Skip("real Jev/Codex verification is opt-in")
	}
	helper := os.Getenv("REPOMESH_TYPESAFE_HELPER_BIN")
	if helper == "" {
		t.Fatal("REPOMESH_TYPESAFE_HELPER_BIN required")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatal("Codex CLI required")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal("live key file unreadable")
	}
	key = bytes.TrimSpace(key)
	defer clear(key)
	f := newTypeSafeFixture(t)
	status, raw := f.request(t, "POST", "/api/projects/"+f.project.ID+"/typesafe", map[string]any{"expectedRevision": 0, "enabled": true, "model": typesafe.DefaultModel, "secret": map[string]string{"mode": "replace", "value": string(key)}}, true)
	if status != 200 {
		t.Fatalf("settings save returned HTTP %d", status)
	}
	var settings typesafe.Settings
	if json.Unmarshal(raw, &settings) != nil {
		t.Fatal("settings response invalid")
	}
	status, raw = f.request(t, "POST", "/api/projects/"+f.project.ID+"/typesafe/check", map[string]int64{"expectedRevision": settings.Revision}, true)
	if status != 200 || json.Unmarshal(raw, &settings) != nil || settings.CheckStatus != "ready" {
		t.Fatalf("real synthetic connection check failed: HTTP %d, status %s", status, settings.CheckStatus)
	}
	report := map[string]any{"sourceCommit": typesafe.SkillCommit, "skillHash": typesafe.SkillHash(), "connectionCheck": settings.CheckStatus, "checkedAt": time.Now().UTC().Format(time.RFC3339)}
	defer func() {
		if out := os.Getenv("REPOMESH_TYPESAFE_LIVE_REPORT"); out != "" {
			report["passed"] = !t.Failed()
			data, _ := json.MarshalIndent(report, "", "  ")
			if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
				t.Error(err)
			}
		}
	}()

	version, _ := exec.Command("codex", "--version").Output()
	report["codexVersion"] = strings.TrimSpace(string(version))
	for _, kind := range []string{"test_agent", "review_agent"} {
		t.Run(kind, func(t *testing.T) {
			runID := "live-" + kind
			g := f.grant(t, runID, kind)
			t.Cleanup(func() { _ = typesafe.FinishGrant(context.Background(), f.pool, runID, "") })
			dir := t.TempDir()
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=TypeSafe Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "synthetic fixture"}} {
				c := exec.Command("git", args...)
				c.Dir = dir
				if c.Run() != nil {
					t.Fatal("cannot initialize owned fixture checkout")
				}
			}
			if err := typesafe.InstallSkill(dir, runID); err != nil {
				t.Fatal(err)
			}
			evidence := "Synthetic test output: test_discount_20_percent PASS: input 100, discount 20, expected 80, observed 80. Process exit code: 0. No shipping, tax or concurrency tests were executed.\nSynthetic reviewed source (discount.py:1-2):\ndef discounted_price(price, percent):\n    return price * (1 - percent / 100)\n"
			if err := os.WriteFile(filepath.Join(dir, "evidence.txt"), []byte(evidence), 0o600); err != nil {
				t.Fatal(err)
			}
			prompt := `Use the TypeSafe skill at .agents/skills/typesafe-ai/SKILL.md. Read that file in a shell tool first. Validate only the synthetic evidence in evidence.txt. Do not edit source code or install anything. The official skill is supplied; this environment uses the RepoMesh helper instead of a provider key.
Create exactly one request file request.json with requestId codex-synthetic-v1 and these claims:
id positive: The synthetic test observed a result of 80 for price 100 and discount 20 percent.
id negative: The synthetic test observed a result of 90 for price 100 and discount 20 percent.
id missing: 项目数据库已开启自动备份。
Include the full contents of evidence.txt as evidence and the full git HEAD as commit.
Run the executable identified by REPOMESH_TYPESAFE_HELPER with arguments typesafe evaluate request.json. Use the inherited REPOMESH_TYPESAFE_GRANT and broker URL implicitly through the helper. Never print environment variables, credentials or read configuration files. Do not call any other model API or contact any other service except the public TypeSafe documentation if the skill requires it. Do not modify the database, approve tasks or merge anything. If the tool fails, report its exact error category instead of substituting your own answer. Finish with the three actual Jev choices and its returned model version.`
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "codex", "exec", "--ephemeral", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "-C", dir, "-")
			cmd.Stdin = strings.NewReader(prompt)
			for _, entry := range os.Environ() {
				name, _, _ := strings.Cut(entry, "=")
				if strings.HasPrefix(name, "REPOMESH_") || strings.HasPrefix(name, "TYPESAFE_") || strings.HasPrefix(name, "JEV_") {
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, "REPOMESH_TYPESAFE_HELPER="+helper, "REPOMESH_TYPESAFE_GRANT="+g.Token, "REPOMESH_TYPESAFE_BROKER_URL="+f.server.URL, "REPOMESH_TYPESAFE_RUN_ID="+runID)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			combined := append(append([]byte{}, stdout.Bytes()...), stderr.Bytes()...)
			if bytes.Contains(combined, key) || bytes.Contains(combined, []byte(g.Token)) {
				t.Fatal("CLI output exposed a credential")
			}
			loaded, invoked := false, false
			for _, line := range bytes.Split(stdout.Bytes(), []byte{'\n'}) {
				var event struct {
					Item struct {
						Type    string `json:"type"`
						Command string `json:"command"`
						Output  string `json:"aggregated_output"`
					} `json:"item"`
				}
				if json.Unmarshal(line, &event) != nil || event.Item.Type != "command_execution" {
					continue
				}
				if strings.Contains(event.Item.Command, "SKILL.md") && strings.Contains(event.Item.Output, "Build with TypeSafe") {
					loaded = true
				}
				if strings.Contains(event.Item.Command, "typesafe evaluate") {
					invoked = true
				}
			}
			if runErr != nil || !loaded || !invoked {
				// A diagnostic log is only written to an explicitly supplied local
				// directory, after ensuring it contains neither credential.
				if out := os.Getenv("REPOMESH_TYPESAFE_LIVE_REPORT"); out != "" {
					_ = os.WriteFile(out+"."+kind+".log", combined, 0o600)
				}
				t.Fatalf("Codex verification failed: process=%v skillRead=%v helperCalled=%v", runErr, loaded, invoked)
			}
			items, err := f.service.List(context.Background(), f.project.OwnerID, f.project.ID, "typesafe-web-issue")
			if err != nil {
				t.Fatal(err)
			}
			var actual *typesafe.Evaluation
			for i := range items {
				if items[i].RunID == runID {
					actual = &items[i]
					break
				}
			}
			report[kind] = map[string]any{"skillRead": loaded, "helperCalled": invoked, "evaluation": actual}
			if actual == nil || actual.Status != "completed" || actual.Response == nil {
				if out := os.Getenv("REPOMESH_TYPESAFE_LIVE_REPORT"); out != "" {
					_ = os.WriteFile(out+"."+kind+".log", combined, 0o600)
				}
				if actual != nil {
					t.Fatalf("real Jev call did not complete: status=%s code=%s", actual.Status, actual.ErrorCode)
				}
				t.Fatal("helper was invoked but no request was accepted; inspect the protected CLI log")
			}
			for id, want := range map[string]string{"positive": "supported", "negative": "contradicted", "missing": "insufficient"} {
				if actual.Response.Answers[id].Choice != want {
					t.Fatalf("real Jev synthetic case %s: got %s", id, actual.Response.Answers[id].Choice)
				}
			}
			t.Logf("Codex loaded official skill and used broker; purpose=%s model=%s inputTokens=%d", actual.Purpose, actual.Response.Model, actual.Response.Usage.InputTokens)
		})
	}
	if !t.Failed() && os.Getenv("REPOMESH_TYPESAFE_BROWSER_SCRIPT") != "" {
		foreign := testdb.SeedProject(t, f.pool, "", "", "fixture/foreign")
		fixtureJSON, _ := json.Marshal(map[string]string{"projectId": f.project.ID, "foreignProjectId": foreign.ID, "issueId": "typesafe-web-issue", "csrf": f.csrf, "cookie": f.cookie, "baseURL": f.server.URL})
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "node", os.Getenv("REPOMESH_TYPESAFE_BROWSER_SCRIPT"))
		cmd.Env = append(os.Environ(), "REPOMESH_TYPESAFE_UI_FIXTURE="+string(fixtureJSON))
		output, err := cmd.CombinedOutput()
		if bytes.Contains(output, key) || bytes.Contains(output, []byte(f.cookie)) {
			t.Fatal("browser verification exposed a credential")
		}
		if err != nil {
			t.Fatalf("browser verification failed: %s", output)
		}
		var browserReport any
		if json.Unmarshal(bytes.TrimSpace(output), &browserReport) != nil {
			t.Fatal("invalid browser verification report")
		}
		report["browser"] = browserReport
		t.Log("real React settings and evidence components passed browser verification")
	}

}
