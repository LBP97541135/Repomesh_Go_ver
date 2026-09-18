# LIVE-10 注销

Time: 2026-09-15 01:32–01:35 UTC. After LIVE-09 snapshot and a post-refresh login used only for `重新发现`.

Proxy: `15/Sep/2026 01:32:58 POST /api/auth/logout 204`. Then `GET /api/session` 401 `AUTHENTICATION_REQUIRED` on `https://127.0.0.1:18443` with no Cookie header. Snapshot `after-live-10.json`: session generation 6 `revoked=true`, `last_active_at` 01:32:58Z. GitHub connection stayed `connected` / idle (logout does not drop the App connection).

Desktop UI after logout: heading `开始使用 RepoMesh`, button `使用 GitHub 登录`, no `仓库与账号连接` list. `/projects` same shell. See `live-10-logout-desktop.webp` and `live-10-logout-projects.webp`.

Narrow window: xdotool requested 390×844; Chrome used 501×844 (minimum width). Still the login shell, list gone. Then restored 1820×1100. See `live-10-logout-narrow.webp`. Signed-in narrow view from 18:06 UTC remains `live-10-mobile-pre.md`.

Cookie values omitted. Private repository names omitted.
