package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/buildinfo"
)

func TestDatabaseCommandArguments(t *testing.T) {
	secret := "postgres://account:secret-password@localhost:bad-port/private"
	for _, test := range []struct {
		name string
		args []string
		env  string
		code int
		want string
	}{
		{"missing subcommand", []string{"db"}, "", 2, "expected db check or db migrate"},
		{"unknown subcommand", []string{"db", secret}, "", 2, "expected db check or db migrate"},
		{"missing connection", []string{"db", "check"}, "", 2, "connection string is required"},
		{"invalid environment", []string{"db", "check"}, secret, 2, "invalid database configuration"},
		{"invalid flag connection", []string{"db", "migrate", "--database-url", secret}, "", 2, "invalid database configuration"},
		{"explicit empty overrides env", []string{"db", "check", "--database-url="}, secret, 2, "connection string is required"},
		{"unknown flag", []string{"db", "check", "--" + secret}, "", 2, "invalid database command arguments"},
		{"invalid timeout", []string{"db", "check", "--timeout", secret}, "", 2, "invalid database command arguments"},
		{"zero timeout", []string{"db", "check", "--timeout=0"}, "", 2, "positive timeout"},
		{"negative timeout", []string{"db", "migrate", "--timeout=-1s"}, "", 2, "positive timeout"},
		{"positional value", []string{"db", "check", secret}, "", 2, "valid flags"},
		{"group help", []string{"db", "--help"}, secret, 0, "Usage: repomesh-web db"},
		{"check help", []string{"db", "check", "--help"}, secret, 0, "REPOMESH_DATABASE_URL"},
		{"migrate help", []string{"db", "migrate", "--help"}, secret, 0, "total connection and operation timeout"},
		{"help after secret", []string{"db", "check", "--database-url", secret, "--help"}, "", 0, "Usage: repomesh-web db check"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("REPOMESH_DATABASE_URL", test.env)
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), test.args, &stdout, &stderr)
			output := stdout.String() + stderr.String()
			if code != test.code || !strings.Contains(output, test.want) {
				t.Fatalf("code=%d output=%q; want code=%d containing %q", code, output, test.code, test.want)
			}
			for _, sensitive := range []string{"secret-password", "account", "bad-port", "/private"} {
				if strings.Contains(output, sensitive) {
					t.Fatal("database command output exposed connection data")
				}
			}
		})
	}
}

func TestDatabaseCommandCanceledConnection(t *testing.T) {
	t.Setenv("REPOMESH_DATABASE_URL", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"db", "check", "--database-url", "postgres://account:secret-password@127.0.0.1:1/private?sslmode=disable"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	if strings.Contains(stderr.String(), "secret-password") {
		t.Fatal("canceled connection exposed password")
	}
}

func TestScaffoldCommandCompatibility(t *testing.T) {
	t.Setenv("REPOMESH_AUTH_CONFIG", "")
	t.Setenv("REPOMESH_DATABASE_URL", "invalid and must not be parsed")
	t.Setenv("REPOMESH_WEB_ASSETS", t.TempDir())
	t.Setenv("REPOMESH_WEB_ADDR", "invalid-address")
	t.Run("version does not require assets or database", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"--version"}, &stdout, &stderr); code != 0 ||
			stdout.String() != "repomesh-web "+buildinfo.Version+"\n" || stderr.Len() != 0 {
			t.Fatalf("version failed; stdout=%q stderr=%q", &stdout, &stderr)
		}
	})
	t.Run("normal serve still requires built assets", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), nil, &stdout, &stderr); code != 1 ||
			!strings.Contains(stderr.String(), "frontend index.html missing") {
			t.Fatalf("serve failed unexpectedly; stdout=%q stderr=%q", &stdout, &stderr)
		}
	})
	t.Run("serve help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 ||
			!strings.Contains(stderr.String(), "-assets") {
			t.Fatalf("serve help failed; stdout=%q stderr=%q", &stdout, &stderr)
		}
	})
	t.Run("unexpected positional argument", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"unexpected"}, &stdout, &stderr); code != 2 {
			t.Fatalf("positional argument exit=%d", code)
		}
	})
}
