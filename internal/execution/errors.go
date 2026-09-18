package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
		return fmt.Sprintf("execution: %s (%s)", f.Code, f.Fields[0].Field)
	}
	return "execution: " + f.Code
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code}
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
