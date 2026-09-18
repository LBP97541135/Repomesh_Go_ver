package messages

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"repomesh.local/repomesh/internal/projects"
)

// Failure is the typed service error.
type Failure struct {
	Status int
	Code   string
	Fields []projects.FieldError
}

func (f *Failure) Error() string {
	if len(f.Fields) > 0 {
		return fmt.Sprintf("messages: %s (%s)", f.Code, f.Fields[0].Field)
	}
	return "messages: " + f.Code
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code}
}

func fieldFailure(field, code string) error {
	return &Failure{Status: 422, Code: "VALIDATION_FAILED", Fields: []projects.FieldError{{Field: field, Code: code}}}
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

func digest(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}

func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", unavailable()
	}
	return prefix + hex.EncodeToString(buffer), nil
}
