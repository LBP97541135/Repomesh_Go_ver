package manager

import (
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
		return fmt.Sprintf("manager: %s (%s)", f.Code, f.Fields[0].Field)
	}
	return "manager: " + f.Code
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code}
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }
