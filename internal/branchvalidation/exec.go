package branchvalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// execOn runs one statement on a database DSN (admin paths only; no result
// rows expected).
func execOn(ctx context.Context, dsn, statement string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, statement); err != nil {
		return err
	}
	return conn.Close(ctx)
}

// replaceDatabase swaps the database name in a URL-form postgres DSN.
func replaceDatabase(dsn, database string) string {
	schemeEnd := strings.Index(dsn, "://")
	if schemeEnd < 0 {
		return dsn
	}
	rest := dsn[schemeEnd+3:]
	pathStart := strings.Index(rest, "/")
	if pathStart < 0 {
		return dsn + "/" + database
	}
	path := rest[pathStart+1:]
	if query := strings.Index(path, "?"); query >= 0 {
		return dsn[:schemeEnd+3+pathStart+1] + database + path[query:]
	}
	return dsn[:schemeEnd+3+pathStart+1] + database
}

func hashOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var _ = fmt.Sprintf
var _ = hashOf
