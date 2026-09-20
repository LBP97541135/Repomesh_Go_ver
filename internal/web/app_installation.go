package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
)

// registerAppInstallation 挂 GitHub App 安装状态读面（只读，2026-09-20）。
//
// 它回答「我的仓都归哪些账号所有、哪些还没装 App、我自己能不能装」——
// 供界面把"要装 App"这件事一次说清、并给出直链，而不是让人在建 issue 时
// 撞上 NO_AVAILABLE_REPOSITORIES 再回来猜。
//
// **只读**：GitHub 没有创建安装的 API（只有列出/读/铸令牌/删），装那一下必须由
// 有权限的人在网页上点。这里不假装能做那件事。
func registerAppInstallation(mux *http.ServeMux, auth Auth) {
	registerProjectRoute(mux, "GET /api/access/app-installation", auth,
		func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			view, err := auth.Service.AppInstallationStatus(r.Context(), principal)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, view)
			return nil
		})
}
