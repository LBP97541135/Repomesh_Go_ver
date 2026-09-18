# 浏览器诊断更正

时间：2026-09-12 PDT

## 更正

此前把 `context.pages()[0]` 操作的新 attempt 页面与 `context.pages().at(-1)` 检查到的旧 attempt 页面误认为同一页面发生了回退。现场实际有两个同源页面，因此“主页面回到旧 attempt”的结论不成立。

此前把 `navigation-02.jsonl` 中的 `frontendValid: true` 称为与前端 `parseStart` 等价也不成立。诊断脚本只使用 `Date.parse` 检查时间，没有执行前端 `timestamp` 的严格 UTC `Z` 后缀约束。已确认旧 attempt 的 `observedAt` 为 `2026-09-12T06:58:16.237684-07:00`；该值虽可由 JavaScript 解析，但不符合前端只接受 `Z` 的规则。根因是数据库读取 `timestamptz` 时没有统一到 UTC，前端因此将真实响应判为 `INVALID_RESPONSE`。

关于 session generation、visibility refresh 或组件卸载导致静默放弃跳转的说法没有被本次证据证明，应撤回，不作为根因结论。

## 有界证据

- `navigation-02.jsonl` 原样保留。它证明第二次 start 返回 201、所检查的四个顶层字段存在、30 秒内该页面未到达 GitHub，且没有捕获到 `requestfailed` 或 `pageerror`。
- 独立诊断标签访问 `https://github.com/login` 返回 200，只能证明当时 Chromium 到 GitHub 的基本网络和 TLS 路径可用。
- root 的只读 attempt 响应确认 `observedAt` 使用 `-07:00` 偏移；源码中的前端时间校验只接受 UTC `Z`。这两项共同确认响应被判无效的原因。

## 保留现场

- `page0`: `https://repomesh.bohanxu.me:8443/auth/result/6413121b-1b3c-42a7-8cee-34b8d32cc9d5`
- `page1`: `https://repomesh.bohanxu.me:8443/auth/result/00b02a0f-7ce4-4fda-8e6f-30e2240f1ff3`

后续控制器固定使用 `context.pages()[0]`。本次更正没有导航页面、重新发送 OAuth 请求或创建新 attempt。
