package web

import (
	"context"
	"net/http"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
)

// registerIssueRoutes wires the B07 read surface: the project issue list, the
// minimal issue detail and the rooms view. All are read-only; none create
// objects or start runtime work.
func registerIssueRoutes(mux *http.ServeMux, auth Auth, issueAPI Issues) {
	registerProjectRoute(mux, "GET /api/projects/{projectId}/issues", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return listIssues(w, r, issueAPI.Service, claims)
	})

	// ③ 执行中人工打断的落点：把**人确认过**的新仓库追加进 issue 的仓库范围。
	// 2026-09-20 之前范围只在建 issue 时写入（insertWorkScope），没有任何追加路径 ——
	// "动态引入新仓库"因此整条接不上。这里只做追加，不做"替调用方挂仓库"
	// （挂仓库是另一个动作，服务层会以 409 REPOSITORY_NOT_IN_PROJECT 明确拒绝）。
	registerProjectRoute(mux, "POST /api/projects/{projectId}/issues/{issueId}/scope/repositories", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			RepositoryID string "json:\"repositoryId\""
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		if err := issueAPI.Service.AppendRepository(r.Context(), claims, r.PathValue("projectId"), r.PathValue("issueId"), body.RepositoryID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "appended"})
		return nil
	})
	// 选仓门批量确认(spec 2026-09-20 §3.2):一个事务里双表写 + 门 CAS 置 resolved。
	registerProjectRoute(mux, "POST /api/projects/{projectId}/issues/{issueId}/scope/selection", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return confirmScopeSelection(w, r, issueAPI.Service, claims, issueAPI.Rooms, issueAPI.OnScopeConfirmed)
	})
	mux.HandleFunc("GET /api/issues/{issueId}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil || issueAPI.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		detail, err := issueAPI.Service.GetIssue(ctx, claims, projectID, r.PathValue("issueId"))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
	mux.HandleFunc("GET /api/issues/{issueId}/rooms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil || issueAPI.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		view, err := issueAPI.Service.GetIssueRooms(ctx, claims, projectID, r.PathValue("issueId"))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	// 房间消息。房间在 AgentTeams 的 homeserver 上，不在本库，所以要打出去；
	// 控制器的 REST 里没有"按 roomID 读消息"这一条（它只有
	// /projects/{id}/spawns/{sessionId}/messages，要求先有项目），所以直接读 Matrix。
	//
	// 授权复用 GetIssueRooms：**只允许读这个 issue 关联到的房**。房间号是上游的
	// 不透明 id，凭一个 id 就能读任意房间等于绕过项目边界。
	mux.HandleFunc("GET /api/issues/{issueId}/rooms/{roomId}/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil || issueAPI.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		roomID := r.PathValue("roomId")
		if roomID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		view, err := issueAPI.Service.GetIssueRooms(ctx, claims, projectID, r.PathValue("issueId"))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		if !roomBelongsToIssue(view, roomID) {
			writeProjectError(w, &issues.Failure{Status: 404, Code: "RESOURCE_NOT_FOUND"})
			return
		}
		if issueAPI.Matrix == nil {
			// 没配 Matrix 就如实说没配，不返一个空消息流让界面以为"房间没人说话"。
			writeProjectError(w, &access.Failure{Status: 503, Code: "ROOM_STREAM_NOT_CONFIGURED"})
			return
		}
		limit := atoiDefault(r.URL.Query().Get("limit"), 100)
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		messages, err := issueAPI.Matrix.RoomMessages(ctx, roomID, limit)
		if err != nil {
			// 上游读不到是上游的问题（502），不是"房间空"。
			writeProjectError(w, &access.Failure{Status: 502, Code: "ROOM_STREAM_UNAVAILABLE"})
			return
		}
		items := make([]map[string]any, 0, len(messages))
		for _, message := range messages {
			items = append(items, map[string]any{
				"eventId": message.EventID,
				"sender":  message.Sender,
				"body":    message.Body,
				"at":      message.Timestamp,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"roomId": roomID, "messages": items})
	})
}

// roomBelongsToIssue 判断请求的房间号是否真的是这个 issue 关联到的房。
func roomBelongsToIssue(view issues.RoomsView, roomID string) bool {
	if view.Main.RoomID != nil && *view.Main.RoomID == roomID {
		return true
	}
	for _, entry := range view.Leaders {
		room, ok := entry.(issues.RepositoryRoom)
		if ok && room.RoomID != nil && *room.RoomID == roomID {
			return true
		}
	}
	return false
}

func listIssues(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	projectID := r.PathValue("projectId")
	query, err := issues.ParseIssueListQuery(r.URL.Query().Get("q"), r.URL.Query().Get("repositoryId"), r.URL.Query().Get("cursor"), atoiDefault(r.URL.Query().Get("limit"), 50))
	if err != nil {
		return err
	}
	page, err := service.ListIssues(r.Context(), claims, projectID, query)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, page)
	return nil
}
