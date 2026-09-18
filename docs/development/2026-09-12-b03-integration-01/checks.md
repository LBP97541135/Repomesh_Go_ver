# B03 集成 I0/I1 机械检查

状态：`PASS_WITH_FULL_VALIDATION_PENDING`。本页只证明批准文件已经集成并能通过最小编译和类型检查，不替代 I2 至 I5 的真实 PostgreSQL、浏览器、发布与独立复核。

## 文件和范围

- 427 项基线归档逐项核验，0 个不匹配。
- 旧统计范围重算为 459 个路径：399 个三方未变、58 个 B03-only、2 个双方变更且不同，其余类别为 0。
- 按最新保护规则排除 `docs/archive/` 后统计 423 个路径：363 个三方未变、58 个 B03-only、2 个双方变更且不同。
- 60 个候选全部写入，最终输出哈希不匹配为 0。
- 53 个路径为 B03 来源精确字节，7 份文档为预先散列的裁决结果。
- 对 423 个非保护范围产品路径重新比较基线，实际有 60 个变化，全部位于允许清单；允许清单外变化为 0。
- Git 状态路径从执行前 409 项变为最终快照 447 项。新增状态路径包括 35 个新出现的候选或候选状态，以及本目录的证据文件；意外新增状态路径为 0。

最终 Git 状态完整快照保存在私有回滚目录的 `target-git-status-final.txt`，SHA-256 为 `357bf66def913f1c32cae3a9d0a4a120637714a0e5d94f47a192f31d6fc44669`。I0 清单 SHA-256 为 `046a699d9488c6d08cdc905fef32bf14e73ae57b18bea6d81a8315bdca069c4b`；I1 输出清单 SHA-256 为 `d70961ea517665103fde817c18cc19192ac3d4baf6c944d209d2fb8c1bd60ea2`。

## 执行命令

| 命令 | 结果 | 用途 |
| --- | --- | --- |
| 候选 Go 文件执行 `gofmt -w` | exit 0，输出哈希仍全部匹配 | 机械格式核对 |
| `go build ./...` | exit 0 | 编译三个入口和产品包 |
| `npm --prefix web run typecheck` | exit 0，TypeScript `tsc --noEmit` | 前端类型检查 |
| 候选范围 `git diff --check` | exit 0 | 空白错误检查 |

本轮没有运行 `go test ./...`、B03 PostgreSQL 批次、浏览器、前端构建、发布构建或真实 GitHub 验收。运行中的 r3 Web 和 coordinator 使用旧 r3 发布目录，未切换、重启或迁移。主目录现有 `web/dist` 仍为原 3 个文件；五个旧目标发布目录共 59 个文件，B03 worktree 旧包仍为 12 个文件。

## 保护检查

`.codex/config.toml` 当前 SHA-256 为 `58c3d47686d46de2074c7acc7bb897d41807b4ed89fad8d20482448048e5cbad`，本轮只散列未读正文。second-account-02 的 `PAUSE.md` 当前 SHA-256 为 `3da54c323316e1ae8a2f5ea131c85495cc409958315917886b8e87341b9d7cbc`。本轮没有执行 GitHub 操作，没有访问认证秘密正文，没有更改数据库、服务、浏览器 profile、旧 B02 证据、second-account-02 证据、旧发布包、`validation/` 或 `third_party/`。
