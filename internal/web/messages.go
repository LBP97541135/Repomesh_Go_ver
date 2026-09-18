package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/manager"
	"repomesh.local/repomesh/internal/messages"
)

// Messages carries the message and manager consumption services into the web
// layer; the zero value skips every route.
type Messages struct {
	Service     *messages.MessageService
	Consumption *manager.Service
}

func registerMessages(mux *http.ServeMux, auth Auth, messagesAPI Messages) {
	if messagesAPI.Service == nil {
		return
	}
	registerProjectRoute(mux, "POST /api/projects/{projectId}/conversations/{conversationId}/messages", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return submitMessage(w, r, messagesAPI.Service, claims)
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/conversations/{conversationId}/messages", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return listMessages(w, r, messagesAPI.Service, claims)
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/message-submissions/{submissionId}", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return getMessageSubmission(w, r, messagesAPI.Service, claims)
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/logical-requests/{requestId}/clarification", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return getClarification(w, r, messagesAPI.Service, claims)
	})
}

func submitMessage(w http.ResponseWriter, r *http.Request, service *messages.MessageService, claims access.ProjectPrincipal) error {
	key, err := projectIdempotencyKey(r)
	if err != nil {
		return err
	}
	input, err := readIssueInput(w, r)
	if err != nil {
		return err
	}
	command := messages.SubmissionCommand{
		ProjectID:      r.PathValue("projectId"),
		ConversationID: r.PathValue("conversationId"),
		SubmissionID:   key,
		Actor:          claims.ActorID(),
		Body:           input,
	}
	receipt, replayed, err := service.Submit(r.Context(), claims, command)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, receipt)
	return nil
}

// The message list projection is emitted directly by the service (it encodes
// the JSON itself), so no duplicate wire type is kept here.

func listMessages(w http.ResponseWriter, r *http.Request, service *messages.MessageService, claims access.ProjectPrincipal) error {
	data, err := service.ListMessages(r.Context(), claims, r.PathValue("projectId"), r.PathValue("conversationId"), atoiDefault(r.URL.Query().Get("limit"), 50), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
	return nil
}

func getMessageSubmission(w http.ResponseWriter, r *http.Request, service *messages.MessageService, claims access.ProjectPrincipal) error {
	receipt, err := service.GetSubmission(r.Context(), claims, r.PathValue("projectId"), r.PathValue("submissionId"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, receipt)
	return nil
}

func getClarification(w http.ResponseWriter, r *http.Request, service *messages.MessageService, claims access.ProjectPrincipal) error {
	view, err := service.GetClarification(r.Context(), claims, r.PathValue("projectId"), r.PathValue("requestId"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}
