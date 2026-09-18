package issues

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"repomesh.local/repomesh/internal/projects"
)

// Failure is the typed service error the web layer maps onto the contract error
// envelope.
type Failure struct {
	Status int
	Code   string
	Fields []projects.FieldError
}

func (f *Failure) Error() string {
	if len(f.Fields) > 0 {
		return fmt.Sprintf("issues: %s (%s)", f.Code, f.Fields[0].Field)
	}
	return "issues: " + f.Code
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code}
}

func fieldFailure(field, code string) error {
	return &Failure{Status: 422, Code: "VALIDATION_FAILED", Fields: []projects.FieldError{{Field: field, Code: code}}}
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

// digest is the server-side input fingerprint; the client's own digest is never
// trusted, so this always recomputes from the canonical input.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// newID mints one opaque identifier from cryptographic randomness. Identifiers
// are never derived from user input.
func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", unavailable()
	}
	return prefix + hex.EncodeToString(buffer), nil
}
