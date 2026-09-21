package web

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"repomesh.local/repomesh/internal/access"
)

// observeProxyPrefix 是观测工作台在控制台里的**同源挂载点**。
//
// 工作台自己的路由全是根路径（/style.css、/app.js、/api/...），所以这一层负责
// 剥掉前缀再转发；前端那边也只需要把引用写成相对路径，两种运行形态（独立跑在
// 回环上 / 挂在控制台下）就都能解析对。
const observeProxyPrefix = "/observe"

// observeWorkbenchOrigin 读配置并**只认回环 origin**。
//
// 这条约束来自 ADR 0024（观测本地）：工作台只跑在本机回环上，web 只做转发，
// 从不接受浏览器指定的目标 URL。2026-09-21 把工作台搬到服务器之后，"本机"变成
// **服务器自己的回环** —— 约束一字未改，只是回环换了机器：浏览器永远碰不到工作台
// 的裸端口，也没有任何请求带得出这个 origin。
func observeWorkbenchOrigin() (*url.URL, error) {
	raw := strings.TrimSpace(os.Getenv("REPOMESH_OBSERVE_WORKBENCH_URL"))
	if raw == "" {
		raw = "http://127.0.0.1:18090"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("观测工作台地址不是合法 URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("观测工作台必须是 HTTP(S) origin")
	}
	if parsed.User != nil {
		return nil, errors.New("观测工作台地址不能带凭据")
	}
	if parsed.Port() == "" {
		port := "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
		parsed.Host = net.JoinHostPort(parsed.Hostname(), port)
	}
	if ip := net.ParseIP(parsed.Hostname()); ip == nil || !ip.IsLoopback() {
		return nil, errors.New("观测工作台必须是回环 origin（ADR 0024）")
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
	return parsed, nil
}

// registerObserveProxy 把观测工作台挂到同源的 /observe/ 下。
//
// 为什么需要这一层：控制台的「观测」此前是**先探活再跳转**到
// http://127.0.0.1:18090/ —— 那是**操作者自己机器上的**进程。服务器上装了工作台
// 之后，那一跳仍然指向访问者自己的 127.0.0.1，等于什么都没连上（用户 2026-09-21
// 报的"每次打开都是本地启动，但我本地啥也没有，东西都在服务器"）。
//
// 现在的形态：工作台跑在**服务器回环** 18090，web 在同源 /observe/ 下反代过去。
// 浏览器不再需要任何本机进程，而"工作台只监听回环、外部无法直连"这条性质原样保留。
//
// 鉴权用**管理员会话**：工作台能读全量证据、能改评分模型与 Key，不能对任何登录
// 用户开放。这与 observation_models 那条配置桥同一取向。
func registerObserveProxy(mux *http.ServeMux, auth Auth) {
	origin, originErr := observeWorkbenchOrigin()

	guard := func(w http.ResponseWriter, r *http.Request) bool {
		if auth.Service == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AUTH_NOT_CONFIGURED"})
			return false
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), r.Method != http.MethodGet)
		if err != nil {
			writeObserveProxyError(w, err)
			return false
		}
		admin, err := auth.Service.IsAdmin(r.Context(), principal.ActorID())
		if err != nil {
			writeObserveProxyError(w, err)
			return false
		}
		if !admin {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "ADMIN_REQUIRED",
				"message": "观测工作台读全量证据、能改评分凭据，只对管理员开放。",
			})
			return false
		}
		return true
	}

	var proxy *httputil.ReverseProxy
	if originErr == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// 与环境代理无关：目标是本机回环，走 HTTP_PROXY 会把"本地"这件事说错。
		transport.Proxy = nil
		proxy = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(origin)
				pr.Out.URL.Path = strings.TrimPrefix(pr.Out.URL.Path, observeProxyPrefix)
				if pr.Out.URL.Path == "" {
					pr.Out.URL.Path = "/"
				}
				pr.Out.URL.RawPath = ""
				// 工作台自己要求 Host 是回环（observeui/server.go 的 loopbackHost 门），
				// 而浏览器送来的是 crazykitties.cn —— 不换 Host 会被它 403。
				pr.Out.Host = origin.Host
				// 不把控制台的凭据带过回环边界：工作台不需要它们，带过去只是扩大暴露面。
				pr.Out.Header.Del("Cookie")
				pr.Out.Header.Del("Authorization")
				pr.Out.Header.Del("X-CSRF-Token")
				// Origin/Referer 必须剥掉：浏览器会带 https://crazykitties.cn，而工作台
				// 的 foreign-origin 门只认回环 origin —— 带过去每个写操作都会 403。
				pr.Out.Header.Del("Origin")
				pr.Out.Header.Del("Referer")
			},
			Transport: transport,
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": "OBSERVE_WORKBENCH_UNAVAILABLE",
					"message": "服务器回环上的观测工作台没有应答（" + origin.String() +
						"）。它不是本机进程，起停看 repomesh-observe 服务。",
				})
			},
		}
	}

	mux.HandleFunc("/observe", func(w http.ResponseWriter, r *http.Request) {
		// 工作台用 hash 路由（#overview），尾斜杠不能省 —— 省了相对路径会解析到上一级。
		http.Redirect(w, r, observeProxyPrefix+"/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/observe/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if originErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error":   "OBSERVE_WORKBENCH_MISCONFIGURED",
				"message": originErr.Error(),
			})
			return
		}
		if !guard(w, r) {
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func writeObserveProxyError(w http.ResponseWriter, err error) {
	var failure *access.Failure
	if errors.As(err, &failure) {
		writeJSON(w, failure.Status, map[string]string{"error": failure.Code})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "OBSERVE_PROXY_UNAVAILABLE"})
}
