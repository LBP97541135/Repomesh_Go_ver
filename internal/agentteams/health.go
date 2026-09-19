// health.go extends the AgentTeams adapter with the controller-level health
// and status endpoints (Phase 1 of the health monitoring plan, 2026-09-18).
// These are read-only; the browser gets proxied access via /api/agentteams/*.
package agentteams

import (
	"context"
	"net/http"
)

// ControllerHealth proxies GET /healthz (no auth). Returns the raw body.
// Uses Client.read from workers.go (shared GET helper with Bearer auth);
// healthz is the only path that skips the auth header.
func (c *Client) ControllerHealth(ctx context.Context) ([]byte, int, error) {
	// read() adds Bearer for /api/v1/* paths; /healthz is public upstream
	return c.read(ctx, http.MethodGet, "/healthz")
}

// PlatformStatus proxies GET /api/v1/status (Bearer). Returns kubeMode,
// Worker/Team/Human totals. Raw body passthrough.
func (c *Client) PlatformStatus(ctx context.Context) ([]byte, int, error) {
	return c.read(ctx, http.MethodGet, "/api/v1/status")
}
