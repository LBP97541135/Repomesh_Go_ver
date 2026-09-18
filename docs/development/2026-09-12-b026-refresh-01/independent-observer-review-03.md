# 自然刷新观察器与会话保持复核 03

复核时间：2026-09-12。只复核 `session-keepalive-02.mjs` 对复核 02 所列启动问题的修正。脚本尚未运行，未操作真实数据库、浏览器或服务。

复核 02 的 P1 已关闭。Keeper 在读取 baseline 的唯一有效会话和固定 target 后，每次同源 `/api/session` 结果都要求 HTTP 200 且不含 `errorCode`。错误 origin、网络失败、无效 JSON、401、500、503 或其他非 200 都会写入不含 Cookie 值的记录、退出非零；缺少或不匹配的 user ID 也会退出。它继续以 baseline 中该会话的真实 `expires_at` 作为停止时间，每 10 分钟一次正常 GET。

因此可按既定顺序建立新 baseline、启动 keeper、用只读快照确认 `last_active_at` 前进，再启动 observer。该源码和启动条件复核不构成自然刷新、刷新后发现、移动视口或注销通过证据。
