// Package interface_doc implements M7: the cross-manager interface document
// approval flow (超出 Py 版的新增能力). One manager authors the interface
// document (contract between repositories), the others approve; only a fully
// approved document unlocks the tasks that depend on it. Documents are
// immutable once submitted — a revision opens a new version.
package interfacedoc

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns the interface document approval state machine.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// CreateCommand submits a new interface document version authored by one
// manager, listing every manager who must approve before it takes effect.
type CreateCommand struct {
	ProjectID string
	IssueID   string
	AuthorID  string
	Title     string
	Content   string
	Approvers []string
}

// DocumentView is the read projection.
type DocumentView struct {
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	State       string   `json:"state"`
	AuthorID    string   `json:"authorId"`
	Approvers   []string `json:"approvers"`
	ApprovedBy  []string `json:"approvedBy"`
	EffectiveAt string   `json:"effectiveAt,omitempty"`
	CreatedAt   string   `json:"createdAt"`
}

func unavailable() error { return fmt.Errorf("interfacedoc: database unavailable") }

// wrap surfaces the driver error for debugging instead of hiding it.
func wrap(err error) error {
	return fmt.Errorf("interfacedoc: database unavailable: %w", err)
}
