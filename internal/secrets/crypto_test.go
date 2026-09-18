package secrets

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEnvelopeBinding(t *testing.T) {
	root := make([]byte, 32)
	rand.Read(root)
	plain := []byte("private test token")
	e := envelope{id: "version-a", owner: Owner{Kind: "actor", ID: "alice"}, purpose: GitHubUserToken, format: formatVersion, rootID: "root-a"}
	if err := e.seal(root, plain); err != nil {
		t.Fatal(err)
	}
	opened, err := e.open(root)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatal("matching envelope did not round trip")
	}
	cases := map[string]func(*envelope){
		"version":           func(e *envelope) { e.id = "version-b" },
		"owner kind":        func(e *envelope) { e.owner.Kind = "deployment" },
		"owner ID":          func(e *envelope) { e.owner.ID = "bob" },
		"purpose":           func(e *envelope) { e.purpose = GitHubRefreshToken },
		"format":            func(e *envelope) { e.format = 2 },
		"root ID":           func(e *envelope) { e.rootID = "root-b" },
		"body":              func(e *envelope) { e.ciphertext = bytes.Clone(e.ciphertext); e.ciphertext[len(e.ciphertext)-1] ^= 1 },
		"wrapper":           func(e *envelope) { e.wrappedDEK = bytes.Clone(e.wrappedDEK); e.wrappedDEK[len(e.wrappedDEK)-1] ^= 1 },
		"truncated body":    func(e *envelope) { e.ciphertext = e.ciphertext[:10] },
		"truncated wrapper": func(e *envelope) { e.wrappedDEK = e.wrappedDEK[:10] },
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			changed := e
			alter(&changed)
			if got, err := changed.open(root); err == nil || got != nil {
				t.Fatal("tampered envelope returned plaintext")
			}
		})
	}
	wrong := bytes.Clone(root)
	wrong[0] ^= 1
	if got, err := e.open(wrong); err == nil || got != nil {
		t.Fatal("wrong root returned plaintext")
	}
	second := e
	if err := second.seal(root, plain); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(e.ciphertext, second.ciphertext) || bytes.Equal(e.wrappedDEK, second.wrappedDEK) {
		t.Fatal("repeated sealing reused ciphertext")
	}
	if len(e.wrappedDEK) != 60 || len(e.ciphertext) != len(plain)+28 {
		t.Fatal("nonce or authentication tag is missing")
	}
}

func TestEnvelopeLengthEncodingAndRewrap(t *testing.T) {
	root := make([]byte, 32)
	rand.Read(root)
	next := make([]byte, 32)
	rand.Read(next)
	e := envelope{id: "version-a", owner: Owner{Kind: "ab", ID: "c"}, purpose: AuthMaterial, format: formatVersion, rootID: "root-a"}
	if err := e.seal(root, []byte("state-and-verifier")); err != nil {
		t.Fatal(err)
	}
	changed := e
	changed.owner = Owner{Kind: "a", ID: "bc"}
	if got, err := changed.open(root); err == nil || got != nil {
		t.Fatal("ambiguous owner encoding authenticated")
	}
	body := bytes.Clone(e.ciphertext)
	if err := e.rewrap(root, next, "root-b"); err != nil {
		t.Fatal(err)
	}
	if e.id != "version-a" || !bytes.Equal(body, e.ciphertext) {
		t.Fatal("rewrap changed immutable version or body")
	}
	if got, err := e.open(next); err != nil || string(got) != "state-and-verifier" {
		t.Fatal("rewrapped envelope cannot decrypt")
	}
	if got, err := e.open(root); err == nil || got != nil {
		t.Fatal("old root decrypted new wrapper")
	}
}
