package web

import "net/http"

// Scan carries the scan block's HTTP registration into the server assembly.
// A zero Scan mounts no scan routes (the block is optional infrastructure).
type Scan struct {
	// API mounts the scan endpoints onto the server mux. Implemented by
	// internal/scan's HTTP adapter.
	API interface {
		RegisterRoutes(mux *http.ServeMux)
	}
}

func registerScanRoutes(mux *http.ServeMux, scan Scan) {
	if scan.API != nil {
		scan.API.RegisterRoutes(mux)
	}
}

// SessionCookie exposes the session cookie extractor so sibling blocks can
// authenticate their write endpoints with the same session machinery.
func SessionCookie(r *http.Request) string {
	return cookie(r, sessionCookie)
}
