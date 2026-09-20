package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/humancontrol"
	"repomesh.local/repomesh/internal/spec"
)

// HumanControl carries the ReviewDesk service; the zero value skips every
// route so unconfigured modes keep working.
type HumanControl struct {
	// SpecChanges 是 A2 的落点：审核台上批准一条 checkpoint=spec-change 的待审单
	// 之后，规格升版并触发重规划。Nil = 未接线 —— 那时批准会**如实回 501**，
	// 而不是"批了却什么都没发生"（这正是要避免的那种假成功）。
	SpecChanges SpecChangeApplier
	Service     *humancontrol.Service
}

// SpecChangeApplier 把"人批准了规格变更"落成两件真事：规格升版 + 登记重排。
// 组合根实现它（internal/spec + internal/discovery）。
type SpecChangeApplier interface {
	ApplyApprovedChange(ctx context.Context, projectID, actor, reviewID, evidenceVersion string) (string, error)
}

const sseInterval = 2 * time.Second

// registerHumanControl wires the three session-authenticated endpoints the
// ReviewDesk frontend consumes. Paths keep the /api/v1 prefix the frontend's
// sessionRequest already builds.
func registerHumanControl(mux *http.ServeMux, auth Auth, control HumanControl) {
	if control.Service == nil {
		return
	}
	session := func(w http.ResponseWriter, r *http.Request) (string, bool, error) {
		if auth.Service == nil {
			return "", false, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"}
		}
		principal, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), r.Method != http.MethodGet)
		if err != nil {
			return "", false, err
		}
		admin, err := control.Service.IsAdmin(r.Context(), principal.ActorID())
		return principal.ActorID(), admin, err
	}

	mux.HandleFunc("GET /api/v1/review-requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, admin, err := session(w, r)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		rows, err := control.Service.List(r.Context(), actor, admin, r.URL.Query().Get("status"))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})

	// SSE: poll every 2s and push only when the serialized payload changed,
	// matching the frontend contract (event: review-requests).
	mux.HandleFunc("GET /api/v1/review-requests/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		actor, admin, err := session(w, r)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		last := ""
		for {
			if r.Context().Err() != nil {
				return
			}
			rows, err := control.Service.List(r.Context(), actor, admin, "pending")
			if err == nil {
				payload, _ := json.Marshal(rows)
				if string(payload) != last {
					fmt.Fprintf(w, "event: review-requests\ndata: %s\n\n", payload)
					flusher.Flush()
					last = string(payload)
				}
			}
			timer := time.NewTimer(sseInterval)
			select {
			case <-r.Context().Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})

	mux.HandleFunc("POST /api/v1/projects/{projectId}/checkpoint-decisions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, _, err := session(w, r)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		var body struct {
			ReviewRequestID string `json:"review_request_id"`
			Decision        string `json:"decision"`
			Reason          string `json:"reason"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "INVALID_BODY"})
			return
		}
		view, err := control.Service.Decide(r.Context(), actor, r.PathValue("projectId"), humancontrol.DecisionCommand{
			ReviewRequestID: body.ReviewRequestID, Decision: body.Decision, Reason: body.Reason,
		})
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		// A2：规格变更请求的**批准**是唯一能改 spec 生效的动作（agent 只能提）。
		// 批准之后：规格升版 + 登记重排 v2（由组合根的适配器做）。未接线时如实回 501，
		// 绝不返回 201 让人以为已经生效。
		if view.Checkpoint == spec.ChangeCheckpoint && body.Decision == "approved" {
			if control.SpecChanges == nil {
				writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "SPEC_CHANGE_NOT_WIRED"})
				return
			}
			if _, err := control.SpecChanges.ApplyApprovedChange(r.Context(), r.PathValue("projectId"), actor, view.ReviewRequestID, view.EvidenceVersion); err != nil {
				writeHumanControlError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusCreated, view)
	})

	mux.HandleFunc("POST /api/v1/projects/{projectId}/control", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, _, err := session(w, r)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "INVALID_BODY"})
			return
		}
		if err := control.Service.Control(r.Context(), actor, r.PathValue("projectId"), humancontrol.ControlCommand{Action: body.Action}); err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
	})
}

func writeHumanControlError(w http.ResponseWriter, err error) {
	slog.Warn("humancontrol request failed", "error", err.Error())
	var failure *access.Failure
	if errors.As(err, &failure) {
		writeJSON(w, failure.Status, map[string]string{"error": strings.ToLower(failure.Code)})
		return
	}
	var local *accessFailure
	if errors.As(err, &local) {
		writeJSON(w, local.status, map[string]string{"error": local.code})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_not_found"})
		return
	}
	if errors.Is(err, humancontrol.ErrEvidenceDrifted) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "evidence_drifted"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}
