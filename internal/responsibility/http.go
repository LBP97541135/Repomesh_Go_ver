package responsibility

import (
	"encoding/json"
	"net/http"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{pid}/plans/{planId}/case-timeline", s.timeline)
	mux.HandleFunc("POST /api/projects/{pid}/plans/{planId}/owner-confirm", s.ownerConfirm)
	mux.HandleFunc("POST /api/projects/{pid}/plans/{planId}/auth-request", s.authRequest)
	mux.HandleFunc("POST /api/authorization/{id}/grant", s.authGrant)
	mux.HandleFunc("POST /api/authorization/{id}/revoke", s.authRevoke)
	mux.HandleFunc("POST /api/projects/{pid}/plans/{planId}/transfer", s.transfer)
	mux.HandleFunc("POST /api/projects/{pid}/plans/{planId}/conflicts", s.reportConflict)
	mux.HandleFunc("POST /api/conflicts/{id}/resolve", s.resolveConflict)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) bool {
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func (s *Service) timeline(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planId")
	events, err := s.Timeline(r.Context(), planID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

func (s *Service) ownerConfirm(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planId")
	var body struct {
		RepositoryID string `json:"repository_id"`
		OwnerGithub  string `json:"owner_github_id"`
		ConfirmedBy  string `json:"confirmed_by"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	o, err := s.ConfirmOwner(r.Context(), planID, body.RepositoryID, body.OwnerGithub, body.ConfirmedBy)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, o)
}

func (s *Service) authRequest(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planId")
	var body struct {
		RepositoryID  string `json:"repository_id"`
		RequesterID   string `json:"requester_id"`
		RequesterRole string `json:"requester_role"`
		Scope         string `json:"scope"`
		Reason        string `json:"reason"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	a, err := s.RequestAuth(r.Context(), planID, body.RepositoryID, body.RequesterID, body.RequesterRole, body.Scope, body.Reason)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

func (s *Service) authGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		GrantedBy string `json:"granted_by"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	a, err := s.GrantAuth(r.Context(), id, body.GrantedBy)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s *Service) authRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		RevokedBy string `json:"revoked_by"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	a, err := s.RevokeAuth(r.Context(), id, body.RevokedBy)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s *Service) transfer(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planId")
	var body struct {
		FromRole string `json:"from_role"`
		FromID   string `json:"from_id"`
		ToRole   string `json:"to_role"`
		ToID     string `json:"to_id"`
		Reason   string `json:"reason"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	t, err := s.Transfer(r.Context(), planID, body.FromRole, body.FromID, body.ToRole, body.ToID, body.Reason)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (s *Service) reportConflict(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planId")
	var body struct {
		ConflictType      string `json:"conflict_type"`
		DiscoveredBy      string `json:"discovered_by"`
		DiscoveredByRole  string `json:"discovered_by_role"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	c, err := s.ReportConflict(r.Context(), planID, body.ConflictType, body.DiscoveredBy, body.DiscoveredByRole)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, c)
}

func (s *Service) resolveConflict(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ResolvedBy     string `json:"resolved_by"`
		ResolvedByRole string `json:"resolved_by_role"`
		Resolution     string `json:"resolution"`
	}
	if !decode(r, &body) {
		writeErr(w, 400, "invalid JSON")
		return
	}
	c, err := s.ResolveConflict(r.Context(), id, body.ResolvedBy, body.ResolvedByRole, body.Resolution)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, c)
}
