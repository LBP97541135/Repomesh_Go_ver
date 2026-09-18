package reposcan

import (
	"errors"
	"testing"
)

func TestDetectPlatform(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		extra map[string]string
		want  Platform
		host  string
	}{
		{"github", "https://github.com/acme/orders", nil, PlatformGitHub, "github.com"},
		{"github www", "https://www.github.com/acme/orders", nil, PlatformGitHub, "www.github.com"},
		{"gitlab", "https://gitlab.com/acme/team/orders", nil, PlatformGitLab, "gitlab.com"},
		{"self-hosted declared", "https://git.corp.example.com/acme/orders",
			map[string]string{"git.corp.example.com": "gitlab"}, PlatformGitLab, "git.corp.example.com"},
		{"local drive", `D:\repos\orders`, nil, PlatformLocal, ""},
		{"local relative", "./repos/orders", nil, PlatformLocal, ""},
		{"unsupported", "https://bitbucket.org/acme/orders", nil, PlatformUnsupported, "bitbucket.org"},
		{"unknown host", "https://git.mystery.example.net/acme/orders", nil, PlatformUnknown, "git.mystery.example.net"},
		{"invalid declaration", "https://git.corp.example.com/acme/orders",
			map[string]string{"git.corp.example.com": "sourcehut"}, PlatformUnknown, "git.corp.example.com"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			platform, host, err := DetectPlatform(item.raw, item.extra)
			if platform != item.want {
				t.Fatalf("platform = %v, want %v (err=%v)", platform, item.want, err)
			}
			if host != item.host {
				t.Fatalf("host = %q, want %q", host, item.host)
			}
			if item.want == PlatformUnsupported {
				var unsupported *UnsupportedError
				if !errors.As(err, &unsupported) {
					t.Fatalf("want UnsupportedError, got %v", err)
				}
			}
		})
	}
}

func TestDetectPlatformRejectsEmpty(t *testing.T) {
	if _, _, err := DetectPlatform("   ", nil); err == nil {
		t.Fatal("empty target must be refused")
	}
}

func TestGuard(t *testing.T) {
	allowlist := []string{"github.com", "git.corp.example.com"}

	_, normalized, err := Guard("https://github.com/acme/orders/", allowlist, nil)
	if err != nil || normalized != "https://github.com/acme/orders" {
		t.Fatalf("guard = %q, %v", normalized, err)
	}

	_, normalized, err = Guard("https://github.com/acme/orders.git", allowlist, nil)
	if err != nil || normalized != "https://github.com/acme/orders" {
		t.Fatalf("trailing .git must be stripped, got %q, %v", normalized, err)
	}

	if _, _, err := Guard("https://gitlab.com/acme/orders", allowlist, nil); !errors.Is(err, ErrHostDenied) {
		t.Fatalf("host outside allowlist must be denied, got %v", err)
	}

	if _, _, err := Guard(`D:\repos\orders`, allowlist, nil); !errors.Is(err, ErrLocalPath) {
		t.Fatalf("local path must be refused, got %v", err)
	}

	// A declared self-hosted host passes both platform detection and the
	// allowlist when it is listed.
	if _, _, err := Guard("https://git.corp.example.com/acme/orders", allowlist,
		map[string]string{"git.corp.example.com": "gitlab"}); err != nil {
		t.Fatalf("declared allowlisted host must pass, got %v", err)
	}
}

func TestSplitRepoPath(t *testing.T) {
	segments, ok := SplitRepoPath("https://github.com/acme/orders")
	if !ok || len(segments) != 2 || segments[0] != "acme" || segments[1] != "orders" {
		t.Fatalf("github split = %v, %v", segments, ok)
	}
	segments, ok = SplitRepoPath("https://gitlab.com/group/sub/orders")
	if !ok || len(segments) != 3 {
		t.Fatalf("gitlab split = %v, %v", segments, ok)
	}
	if _, ok := SplitRepoPath("https://github.com/"); ok {
		t.Fatal("bare host must not split")
	}
}
