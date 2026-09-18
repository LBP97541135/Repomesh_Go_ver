# 首次发起后的跳转超时

2026-09-12 13:55:22.585 UTC，专用浏览器真实点击“使用 GitHub 登录”，同源 `POST /api/auth/github/login` 返回 HTTP 201，attempt ID 为 `00b02a0f-7ce4-4fda-8e6f-30e2240f1ff3`。

控制器等待跳转到不同 origin 的 15 秒期限超时，输出固定 `browser control failed`。随后 inspect 仍报告本地 `/auth/result/{attemptId}`；该版 inspect 会重载本地页，可能影响尚未结束的导航，因此不能仅凭该观察判定根因。后续控制器应使用不重载的观察。

2026-09-12 06:56:36.490719 PDT 的数据库只读快照确认该 attempt 为 login/pending，reason 和 observed_at 为空；connections 与 sessions 均为零。

独立网络诊断：Linux curl 不使用代理、不跳过证书校验，访问 `https://github.com/login` 返回 200（1.83 秒）、`https://api.github.com` 返回 200（1.27 秒），TLS 验证值均为 0。

导航原因仍待定位。这不是 GitHub 的实际取消，也不是授权成功；LIVE-02 和 LIVE-03 不据此记 PASS。保留此次请求和超时，不修改数据库、重放令牌或覆盖旧结果。
