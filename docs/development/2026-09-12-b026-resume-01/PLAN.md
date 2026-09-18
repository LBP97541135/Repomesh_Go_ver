### Session pickup

**You own the resume point. Read the prior trail, don't redo it.**

1. Locate the prior trail. A local transcript under Claude Code's per-project transcripts directory at `~/.claude/projects/<encoded-cwd>/*.jsonl` (where `<encoded-cwd>` is the workspace cwd with `/` → `-`; do not glob across other directories under `~/.claude/projects/`, that crosses workspace boundaries and reads private chats from unrelated projects), a cloud-agent URL, or a pushed branch. Read the metadata overview and last messages first, then scan back for the decision points. Parse a long transcript in a subagent and keep the reduced timeline in the main thread (the **principle-guard-the-context-window** skill).
2. Reconstruct operational state. The branch and worktree, what already landed (`git log`, `git diff` against the base), the open todos, the decisions made. The prior trail is authoritative input. Resist the bias to re-derive it.
3. Diff done vs pending. Compare what shipped against what was planned, name the resume point, do not re-run the prior repro or redo completed work. A "let me verify from scratch" pass means you're treating the trail as untrustworthy when it's authoritative.
4. Route the remaining work to the matching playbook and pick the verdict: continue the execution, ship a finished recommendation, ratify or override a prior conclusion, or postmortem a failed run. The pickup playbook ends here; the routed playbook owns the rest.
5. Verify the inherited claims against the original goal on the real artifact (the **principle-prove-it-works** skill). A passing prior self-report is not the proof.

**Reply:** where the prior agent stopped, what you inherited vs redid (ideally nothing redone), the resume point, and the outcome.

## 本轮待办与边界

- [x] 确认 Linux、工作目录、Git 状态并读取用户指定的全部 18 份文件。
- [x] 确认接续点为 B02.6，保留最终本地结果和历史失败记录。
- [x] 核对源码、r1 包及历史证据，另存本轮记录。
- [x] 检查必要配置存在性。当前未设置认证和数据库环境变量，两个约定 auth.json 路径不存在。
- [x] 独立复核本轮记录并更新交接。gpt-5.6 terra 对接手记录范围判定 PASS，真实验收保持未完成。
- [ ] 配置就绪后，按已有模板在新的真实运行目录执行 LIVE-01 至 LIVE-10。
- [ ] B02.6 完整通过及独立复核后，更新 B02 为 VERIFIED，再核对 B03 契约并实施。

## 吞吐检查点

- Blocking first steps. 配置存在性核对先行。真实启动需要固定 HTTPS origin、服务端配置及专用数据库。账号持有人完成 GitHub 登录、同意和组织审批。
- Independent workstreams. GPT-6 Astra 负责规划和接续判断。gpt-5.6 sol 在专属临时目录实现和执行核对脚本。独立复核者核对实际记录。
- Shared mutable state. 主代理只集成本轮新目录和当前交接。执行代理只写专属临时目录，复核者只读仓库。既有源码、.codex/config.toml、旧证据和 r1 包保持原字节。
- Smallest safe decomposition. 先完成接手核对。配置不足时保留 B02.6 BLOCKED，不重跑本地验收，也不提前进入 B03。无新增产品函数或接口设计。

Session pickup 的原步骤保留在上方，实际执行状态以本节为准。已读既有交接作为 prior trail，没有读取其他任务或其他项目的聊天。当前后续真实验收因配置不足尚不能运行，不能将本轮核对算作 LIVE 场景通过。
