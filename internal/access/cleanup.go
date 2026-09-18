package access

import (
	"context"
	"time"

	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) discard(refs ...secrets.VersionID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, ref := range refs {
		if ref != "" {
			_ = s.secrets.Destroy(ctx, ref)
		}
	}
}

func (s *Service) cleanOrphans(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT v.version_id FROM repomesh_secrets.versions v
	WHERE v.destroyed_at IS NULL AND v.created_at<now()-interval '15 minutes'
	AND ((v.owner_kind='auth-attempt' AND v.purpose IN ('auth-material','auth-exchange-result'))
	OR (v.owner_kind='actor' AND v.purpose IN ('github-user-token','github-refresh-token'))
	OR (v.owner_kind='github-app' AND v.purpose IN ('github-app-client-secret','github-app-private-key')))
	AND NOT EXISTS(SELECT 1 FROM repomesh_access.attempts a WHERE a.material_ref=v.version_id OR a.token_ref=v.version_id)
	AND NOT EXISTS(SELECT 1 FROM repomesh_access.connections c WHERE c.access_ref=v.version_id OR c.refresh_ref=v.version_id)
	AND NOT EXISTS(SELECT 1 FROM repomesh_access.app_credentials c WHERE c.version_ref=v.version_id)
	ORDER BY v.created_at LIMIT 20`)
	if err != nil {
		return unavailable()
	}
	var refs []secrets.VersionID
	for rows.Next() {
		var ref secrets.VersionID
		if rows.Scan(&ref) != nil {
			rows.Close()
			return unavailable()
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable()
	}
	for _, ref := range refs {
		if err := s.secrets.Destroy(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
