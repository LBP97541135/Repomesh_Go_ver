package decisionchain

import (
	"strings"
	"testing"
)

func TestNormalizeRequirementCollapsesWhitespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  a  b\t\nc  ", "a b c"},
		{"中文　test", "中文 test"}, // full-width space is whitespace too
		{"plain", "plain"},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := NormalizeRequirement(c.in); got != c.want {
			t.Errorf("NormalizeRequirement(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRequirementKeyStableAndDistinct(t *testing.T) {
	a := RequirementKey(NormalizeRequirement("  为项目选择  合适的仓库  "))
	b := RequirementKey(NormalizeRequirement("为项目选择 合适的仓库"))
	if a != b {
		t.Fatalf("whitespace differences must share a key: %q vs %q", a, b)
	}
	if c := RequirementKey("为项目挑选 合适的仓库"); c == a {
		t.Fatal("different requirements must not share a key")
	}
	if len(a) != 32 || strings.ToLower(a) != a {
		t.Fatalf("key must be 32 lowercase hex chars, got %q", a)
	}
}

func TestNewUUIDv4Shape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewUUIDv4()
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("malformed uuid %q", id)
		}
		if id[14] != '4' || (id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b') {
			t.Fatalf("not a v4 uuid %q", id)
		}
		if seen[id] {
			t.Fatalf("uuid collision %q", id)
		}
		seen[id] = true
	}
}
