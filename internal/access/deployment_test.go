//go:build linux

package access

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

func deploymentContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func deploymentJSONFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal("write temporary deployment configuration failed")
	}
	return path
}

func deploymentConfigFile(t *testing.T, config Deployment) string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal("encode temporary deployment configuration failed")
	}
	return deploymentJSONFile(t, data)
}

func temporaryDeployment(t *testing.T) Deployment {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "临时认证")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal("create temporary credential directory failed")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate temporary RSA key failed")
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	root := make([]byte, 32)
	rand.Read(root)
	client := make([]byte, 32)
	rand.Read(client)
	files := map[string][]byte{"root.key": root, "client.secret": []byte(base64.RawURLEncoding.EncodeToString(client) + "\n"), "private.pem": private}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal("write temporary credential failed")
		}
	}
	config := Deployment{Origin: "https://repomesh.example", AppID: "123456", ClientID: "Iv1.deployment-test", CallbackURL: "https://repomesh.example/api/auth/github/callback",
		ClientSecretFile: filepath.Join(dir, "client.secret"), PrivateKeyFile: filepath.Join(dir, "private.pem"), ActiveRootID: "deployment-root"}
	config.Roots = append(config.Roots, struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}{ID: config.ActiveRootID, Path: filepath.Join(dir, "root.key")})
	return config
}

func rejectDeployment(t *testing.T, runtime *Runtime, err error, want string) {
	t.Helper()
	if runtime != nil {
		runtime.Close()
		t.Fatal("invalid deployment returned a runtime")
	}
	if err == nil || err.Error() != want {
		t.Fatalf("deployment rejection = %v, want %q", err, want)
	}
}

func TestDeploymentRejectsInvalidJSON(t *testing.T) {
	cases := map[string][]byte{
		"invalid UTF-8":         append(append([]byte(`{"origin":"https://`), 0xff), []byte(`.example"}`)...),
		"unknown key":           []byte(`{"unknownConfiguration":true}`),
		"duplicate key":         []byte(`{"appId":"123","appId":"456"}`),
		"escaped duplicate key": []byte(`{"appId":"123","\u0061ppId":"456"}`),
		"nested unknown key":    []byte(`{"roots":[{"id":"one","path":"/unused","unknown":true}]}`),
		"nested duplicate key":  []byte(`{"roots":[{"id":"one","id":"two","path":"/unused"}]}`),
		"unpaired surrogate":    []byte(`{"appId":"\ud800"}`),
		"trailing document":     []byte(`{} {}`),
		"over size limit":       bytes.Repeat([]byte(" "), 1024*1024+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			runtime, err := OpenRuntime(deploymentContext(t), deploymentJSONFile(t, data), "")
			rejectDeployment(t, runtime, err, "invalid authentication configuration")
		})
	}
}

func TestDeploymentRequiresExactHTTPSCallback(t *testing.T) {
	const good = "https://repomesh.example"
	const callback = "/api/auth/github/callback"
	cases := []struct{ name, origin, callback string }{
		{"HTTP origin", "http://repomesh.example", "http://repomesh.example" + callback},
		{"missing host", "https://", "https://" + callback},
		{"userinfo", "https://user@repomesh.example", "https://user@repomesh.example" + callback},
		{"origin path", good + "/nested", good + "/nested" + callback},
		{"origin trailing slash", good + "/", good + "/" + callback},
		{"origin query", good + "?tenant=1", good + "?tenant=1" + callback},
		{"origin fragment", good + "#tenant", good + "#tenant" + callback},
		{"HTTP callback", good, "http://repomesh.example" + callback},
		{"callback host", good, "https://other.example" + callback},
		{"callback path", good, good + "/different-callback"},
		{"callback query", good, good + callback + "?next=elsewhere"},
		{"callback fragment", good, good + callback + "#fragment"},
		{"callback encoded path", good, good + "/api/auth/github/%63allback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := Deployment{Origin: tc.origin, CallbackURL: tc.callback}
			runtime, err := OpenRuntime(deploymentContext(t), deploymentConfigFile(t, config), "")
			rejectDeployment(t, runtime, err, "authentication requires an exact HTTPS origin and callback")
		})
	}
	for _, name := range []string{"certificate only", "key only"} {
		t.Run(name, func(t *testing.T) {
			config := Deployment{Origin: good, CallbackURL: good + callback}
			if name == "certificate only" {
				config.TLSCertificateFile = "/unused.crt"
			} else {
				config.TLSKeyFile = "/unused.key"
			}
			runtime, err := OpenRuntime(deploymentContext(t), deploymentConfigFile(t, config), "")
			rejectDeployment(t, runtime, err, "HTTPS certificate and key must be configured together")
		})
	}
}

type deploymentSecrets struct {
	refs            map[string]string
	versions, wraps int64
}

func readDeploymentSecrets(t *testing.T, ctx context.Context, pool *pgxpool.Pool) deploymentSecrets {
	t.Helper()
	state := deploymentSecrets{refs: make(map[string]string)}
	rows, err := pool.Query(ctx, `SELECT purpose,version_ref FROM repomesh_access.app_credentials ORDER BY purpose`)
	if err != nil {
		t.Fatal("read deployment credential references failed")
	}
	defer rows.Close()
	for rows.Next() {
		var purpose, ref string
		if err := rows.Scan(&purpose, &ref); err != nil {
			t.Fatal("decode deployment credential reference failed")
		}
		state.refs[purpose] = ref
	}
	if rows.Err() != nil {
		t.Fatal("read deployment credential references failed")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_secrets.versions`).Scan(&state.versions); err != nil {
		t.Fatal("count deployment versions failed")
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(wrap_count),0) FROM repomesh_secrets.root_keys`).Scan(&state.wraps); err != nil {
		t.Fatal("count deployment wraps failed")
	}
	return state
}

func TestPostgresDeploymentAssemblesAndReusesSecretsAcrossRestarts(t *testing.T) {
	pool := testdb.Open(t)
	ctx := deploymentContext(t)
	config := temporaryDeployment(t)
	path := deploymentConfigFile(t, config)
	var first deploymentSecrets
	for startup := 0; startup < 3; startup++ {
		runtime, err := OpenRuntime(ctx, path, pool.Config().ConnString())
		if err != nil {
			t.Fatalf("local deployment assembly failed at startup %d: %v", startup, err)
		}
		func() {
			defer runtime.Close()
			if runtime.Service == nil || runtime.Service.provider == nil || runtime.Deployment.Origin != config.Origin {
				t.Fatal("runtime dependencies or deployment origin are missing")
			}
			authorization, err := url.Parse(runtime.Service.provider.AuthorizationURL("local-state", "local-challenge"))
			if err != nil || authorization.Host != "github.com" || authorization.Query().Get("client_id") != config.ClientID || authorization.Query().Get("redirect_uri") != config.CallbackURL {
				t.Fatal("provider assembly lost the configured client or callback")
			}
			current := readDeploymentSecrets(t, ctx, pool)
			if len(current.refs) != 2 || current.versions != 2 || current.wraps != 3 {
				t.Fatalf("credential refs/versions/wraps = %d/%d/%d, want 2/2/3", len(current.refs), current.versions, current.wraps)
			}
			if startup == 0 {
				first = current
			} else if !maps.Equal(first.refs, current.refs) || first.versions != current.versions || first.wraps != current.wraps {
				t.Fatal("restart replaced App secret versions or consumed new wrapping quota")
			}
			for purpose, file := range map[string]string{"github-app-client-secret": config.ClientSecretFile, "github-app-private-key": config.PrivateKeyFile} {
				want, err := os.ReadFile(file)
				if err != nil {
					t.Fatal("read temporary credential for comparison failed")
				}
				if purpose == "github-app-client-secret" {
					want = bytes.TrimSpace(want)
				}
				plain, err := runtime.Service.secrets.Open(ctx, secrets.VersionID(current.refs[purpose]), secrets.Owner{Kind: "github-app", ID: config.AppID}, secrets.Purpose(purpose))
				if err != nil || !bytes.Equal(plain, want) {
					t.Fatal("assembled credential did not decrypt to the imported file content")
				}
				if purpose == "github-app-private-key" {
					block, _ := pem.Decode(plain)
					if block == nil {
						t.Fatal("stored temporary RSA key lost its PEM encoding")
					}
					if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
						t.Fatal("stored temporary RSA key is invalid")
					}
				}
				clear(plain)
				clear(want)
			}
		}()
	}
}

func TestPostgresDeploymentRequiresExplicitMigration(t *testing.T) {
	pool := testdb.Open(t)
	ctx := deploymentContext(t)
	if _, err := pool.Exec(ctx, `DROP SCHEMA repomesh_access CASCADE; DROP SCHEMA repomesh_secrets CASCADE; DELETE FROM public.repomesh_schema_migrations WHERE version>1`); err != nil {
		t.Fatal("prepare isolated database at bootstrap migration failed")
	}
	runtime, err := OpenRuntime(ctx, deploymentConfigFile(t, temporaryDeployment(t)), pool.Config().ConnString())
	rejectDeployment(t, runtime, err, "authentication schema is pending; run db migrate explicitly")
	var count int
	var absent bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.repomesh_schema_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatal("runtime changed the pending migration history")
	}
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname IN ('repomesh_access','repomesh_secrets'))`).Scan(&absent); err != nil || !absent {
		t.Fatal("runtime implicitly created pending authentication schemas")
	}
}

func TestPostgresDeploymentRejectsUnsafeCredentialFiles(t *testing.T) {
	for _, field := range []string{"client secret", "RSA private key", "root"} {
		for _, kind := range []string{"readable by others", "symlink"} {
			t.Run(field+"/"+kind, func(t *testing.T) {
				pool := testdb.Open(t)
				config := temporaryDeployment(t)
				path := &config.ClientSecretFile
				if field == "RSA private key" {
					path = &config.PrivateKeyFile
				} else if field == "root" {
					path = &config.Roots[0].Path
				}
				if kind == "readable by others" {
					if err := os.Chmod(*path, 0644); err != nil {
						t.Fatal("prepare unsafe credential permissions failed")
					}
				} else {
					link := filepath.Join(t.TempDir(), "credential-link")
					if err := os.Symlink(*path, link); err != nil {
						t.Fatal("prepare credential symlink failed")
					}
					*path = link
				}
				runtime, err := OpenRuntime(deploymentContext(t), deploymentConfigFile(t, config), pool.Config().ConnString())
				want := "App credential file must be an absolute, owned, regular 0600 file"
				if field == "root" {
					want = secrets.ErrConfiguration.Error()
				}
				rejectDeployment(t, runtime, err, want)
				if strings.Contains(err.Error(), *path) {
					t.Fatal("credential path escaped in startup error")
				}
			})
		}
	}
}

func TestPostgresDeploymentRejectsMissingAndChangedRoots(t *testing.T) {
	pool := testdb.Open(t)
	ctx := deploymentContext(t)
	config := temporaryDeployment(t)
	runtime, err := OpenRuntime(ctx, deploymentConfigFile(t, config), pool.Config().ConnString())
	if err != nil {
		t.Fatal("initial local deployment assembly failed")
	}
	runtime.Close()
	before := readDeploymentSecrets(t, ctx, pool)
	for _, name := range []string{"missing file", "missing active root", "changed root fingerprint"} {
		t.Run(name, func(t *testing.T) {
			changed := config
			changed.Roots = slices.Clone(config.Roots)
			switch name {
			case "missing file":
				changed.Roots[0].Path = filepath.Join(t.TempDir(), "missing-root.key")
			case "missing active root":
				changed.Roots = nil
			case "changed root fingerprint":
				key := make([]byte, 32)
				rand.Read(key)
				changed.Roots[0].Path = filepath.Join(t.TempDir(), "replacement-root.key")
				if err := os.WriteFile(changed.Roots[0].Path, key, 0600); err != nil {
					t.Fatal("write replacement root failed")
				}
			}
			runtime, err := OpenRuntime(ctx, deploymentConfigFile(t, changed), pool.Config().ConnString())
			if runtime != nil {
				runtime.Close()
				t.Fatal("invalid root configuration returned a runtime")
			}
			if !errors.Is(err, secrets.ErrConfiguration) {
				t.Fatalf("invalid root returned %v, want safe configuration rejection", err)
			}
			after := readDeploymentSecrets(t, ctx, pool)
			if !maps.Equal(before.refs, after.refs) || before.versions != after.versions || before.wraps != after.wraps {
				t.Fatal("failed root configuration altered saved credentials or wrapping count")
			}
		})
	}
	resumed, err := OpenRuntime(ctx, deploymentConfigFile(t, config), pool.Config().ConnString())
	if err != nil {
		t.Fatal("original valid deployment did not recover after rejected root configurations")
	}
	resumed.Close()
}

func TestPostgresDeploymentInvalidAppDoesNotImportSecrets(t *testing.T) {
	pool := testdb.Open(t)
	ctx := deploymentContext(t)
	config := temporaryDeployment(t)
	config.AppID = "not-an-app-id"
	runtime, err := OpenRuntime(ctx, deploymentConfigFile(t, config), pool.Config().ConnString())
	if runtime != nil {
		runtime.Close()
		t.Fatal("invalid App configured")
	}
	if err == nil {
		t.Fatal("invalid App ID accepted")
	}
	state := readDeploymentSecrets(t, ctx, pool)
	if len(state.refs) != 0 || state.versions != 0 || state.wraps != 0 {
		t.Fatal("rejected App configuration imported secrets or activated a root")
	}
}
