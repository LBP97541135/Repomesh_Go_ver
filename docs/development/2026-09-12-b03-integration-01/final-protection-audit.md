# B03 集成最终保护审计说明

状态：`PASS_PROTECTION_AUDIT`。本说明与同目录的 `final-protection-audit.tsv`、`final-protection-audit.json` 记录 I2–I4 完成后的只读保护核对；独立复核结论由复核者单独记录。

## 产品范围

- 主目录 `HEAD` 仍为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`，`git diff --cached --quiet` 为 exit 0，暂存区没有差异。
- 固定 I0 产品与设计范围仍为 423 个路径：363 个与 427 项归档基线相同，60 个发生变化，变化全部属于批准的 60 个候选，允许清单外为 0。
- TSV 对 423 个路径逐项记录基线、B03 来源、I1 输出和最终 SHA-256。60 个候选中，53 个原为来源精确字节，7 个原为 Astra 裁决结果。
- 53 个来源精确候选中，48 个保持 I1 输出，5 个按 Astra 批准的 I2 缺陷修正前进：`Profiles()` 活动 rows 修复、真实 PostgreSQL/HTTP 回归夹具、创建页操作锁、项目页失权清理、设置页操作锁与失权清理。
- 7 个裁决候选中，`AGENTS.md` 保持 I1 输出；根 `README.md` 和五份 `docs/current/` 文档由 Astra 按 I2–I4 结果及最终独立复核继续更新。候选中没有未经批准的最终哈希偏移。

## 保护对象与运行中服务

- `.codex/config.toml` 仅做字节散列，SHA-256 仍为 `58c3d47686d46de2074c7acc7bb897d41807b4ed89fad8d20482448048e5cbad`。
- `docs/development/2026-09-12-b026-second-account-02/PAUSE.md` 仅做字节散列，SHA-256 仍为 `3da54c323316e1ae8a2f5ea131c85495cc409958315917886b8e87341b9d7cbc`。
- 旧五个主目录发布目录仍共 59 个文件。新 `repomesh-0.3.0-b03-integrated-20260912-r1` 为独立第六个目录，共 12 个文件；没有覆盖旧目录。
- B02 Web PID `287037` 和 coordinator PID `287038` 仍从 `repomesh-0.2.0-b026-cookie-20260912-r3` 运行。审计没有停止、切换或修改这些进程。
- 审计没有读取认证秘密或 `.codex` 配置正文。

## 另行计数

JSON 逐文件列出本轮集成验证目录中的脚本、记录与证据及其 SHA-256。浏览器脚本、浏览器 runner、发布 runner 和本审计生成器标记为 `validation_script`；其余文件按集成记录或验证证据分类。

B04 的三个文档单独列入 JSON 的 `b04DocumentsCountedSeparately`，不计入固定 423 路径的 B03 越界判断：

- `docs/current/NEXT-TASK-B04-PROMPT.md`
- `docs/development/2026-09-12-b04-handoff-01/HANDOFF.md`
- `docs/development/2026-09-12-b04-handoff-01/NEXT-SESSION-PROMPT.md`

生成命令：

```text
python3 docs/development/2026-09-12-b03-integration-01/generate-final-protection-audit.py
```

生成器包含强制断言：423/60/53/7 数量、允许清单外 0、候选未经批准偏移 0、固定 HEAD、暂存区无差异、两个保护文件哈希不变、旧发布 59 文件、新发布 12 文件，以及两个 B02 进程命令仍指向 r3。任一断言失败时退出非零。
