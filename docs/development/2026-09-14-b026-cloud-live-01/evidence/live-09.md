# LIVE-09 自然刷新

Time: 2026-09-15 01:28 UTC snapshot, compared with `snapshots/pre-live-09.json`.

Did not edit `access_expires_at`. Did not reconnect. Coordinator and Web PIDs in `/tmp/repomesh-verify-20260914T115738-48932` stayed up.

| 项 | 基线 `pre-live-09.json` | 窗口后 `after-live-09.json` |
| --- | --- | --- |
| `access_epoch` | 6 | 7 |
| `revision` | `c909b3e9-fe5b-432c-af62-78bf4810baae` | `bb7d66a9-152c-42cb-8016-08e112fbe1cf` |
| `status` / `refresh_state` | connected / idle | connected / idle |
| `access_expires_at` | `2026-09-15T01:26:40Z` | `2026-09-15T09:27:21Z` |
| `credential_committed_at` | `2026-09-14T17:26:41Z` | `2026-09-15T01:27:21Z` |
| login confirmed count | 5 | 5 |
| reconnect confirmed count | 1 | 1 |
| latest reconnect | `e8271f6b-71f0-4653-ad10-f0fb6dfa54fa` | same |

Latest discovery in that snapshot was still epoch 6 (old delivered batch). Browser session that waited overnight was already revoked: proxy `14/Sep/2026 18:40:10 POST /api/auth/logout 204` (generation 4). Keepalive PID was dead. Natural token refresh still committed at 01:27:21Z without a live browser session.

After the LIVE-09 snapshot, a new login `66522553-47ed-4dc6-9c66-5b7da6fe42bc` was used only to click `重新发现` (not `重新连接 GitHub`). That login is after the LIVE-09 proof. Result: `snapshots/after-live-09-rediscover.json` discovery epoch 8, 15 items, fixture present. UI: `GitHub 已连接`, `部分发现`, fixture pill `App 能力已核实`. Full list screenshots are not stored here (private names).

See `live-09-post-refresh-login.webp` and `live-09-10-proxy.txt`.
