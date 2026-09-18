# B04 收口后 P1／P2 修复 01

状态：收口后缺陷修复，已用夹具与真实 PostgreSQL 验证。不改写 [B04 收口 01](../2026-09-13-b04-closeout-01/README.md) 的当时结论。`businessReady=false`。非整批 `VERIFIED`。没有真实模型请求。

收口合并后，独立复核在已合并代码上发现一项 P1 与三项 P2。本目录记录对应修复的验证。

## 修复

- 校验未通过的保存留在 `/settings/models`，显示 `VALIDATION_FAILED`，保留非秘密字段，密钥仍发出即清。
- 迁移 `0006_complete_save_vault.sql` 收紧 replace／keep 的 vault 不变量；清理墓碑不再要求 `target`。
- `RemoveSaveResult` 可销毁拒绝回执上的 operation-input vault。
- 采用锁序注释为 槽 → catalog → Provider。

## 检查

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./... -count=1` | 260 通过、0 失败、2 跳过 | [go-test-counts.txt](go-test-counts.txt)、[go-test.txt](go-test.txt)。跳过项仍是 `TestRootFileOwner`、`TestProjectBrowserServer` |
| `go vet ./...` | 退出 0 | 与完整 `go test` 同一次会话 |
| `npm --prefix web test` | 32 通过、0 失败 | 含 `shouldOpenSaveRecovery` |
| `npm --prefix web run typecheck` 与 `build` | 退出 0 | 夹具使用本次 `web/dist` |
| 夹具浏览器 | `browser_exit=0`，`go_test_exit=0`，含 U04.2 校验留页 | [browser-b04-validation/](browser-b04-validation/) |

U04.2 提交 `http://gateway.example.invalid/v1` 后仍停在 `/settings/models`，alert 为「这次保存没有受理…」，名称／模型仍在，API Key 已空。随后原 U04.4 丢响应恢复、空槽 close、跨 actor 404 仍通过。网络计数含 `422:VALIDATION_FAILED=1`。
