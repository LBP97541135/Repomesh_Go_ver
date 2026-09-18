# B02.6 接手与外部验收准备

本轮按 Session pickup 接手。以下保留 playbook 原步骤，执行状态另列。历史交接与最终证据是 prior trail，不读取其他项目的会话。

1. Locate the prior trail. A local transcript under Claude Code's per-project transcripts directory at `~/.claude/projects/<encoded-cwd>/*.jsonl` (where `<encoded-cwd>` is the workspace cwd with `/` → `-`; do not glob across other directories under `~/.claude/projects/`, that crosses workspace boundaries and reads private chats from unrelated projects), a cloud-agent URL, or a pushed branch. Read the metadata overview and last messages first, then scan back for the decision points. Parse a long transcript in a subagent and keep the reduced timeline in the main thread (the **principle-guard-the-context-window** skill).
2. Reconstruct operational state. The branch and worktree, what already landed (`git log`, `git diff` against the base), the open todos, the decisions made. The prior trail is authoritative input. Resist the bias to re-derive it.
3. Diff done vs pending. Compare what shipped against what was planned, name the resume point, do not re-run the prior repro or redo completed work. A "let me verify from scratch" pass means you're treating the trail as untrustworthy when it's authoritative.
4. Route the remaining work to the matching playbook and pick the verdict: continue the execution, ship a finished recommendation, ratify or override a prior conclusion, or postmortem a failed run. The pickup playbook ends here; the routed playbook owns the rest.
5. Verify the inherited claims against the original goal on the real artifact (the **principle-prove-it-works** skill). A passing prior self-report is not the proof.

## 本轮执行

- [x] 确认 Linux、工作目录及 Git 状态，保留既有修改。
- [x] 完整读取用户指定的八组文档及现行索引。
- [x] 从原 JSON 重算最终验证统计，对照源码清单及 r1 发布包哈希。
- [x] 只检查配置是否存在，不读取或输出真实秘密。
- [x] 写明 GitHub App、固定 HTTPS、秘密文件、数据库与两个进程的配置步骤。
- [x] 准备真实授权、取消、同账号重连、令牌刷新、安装权限与私仓发现的验收表。
- [x] 独立复核新增文档、来源及本轮决策记录。
- [x] 更新当前交接，只核对新增文档及证据，不重跑已通过的产品检查。
- [ ] B02.6 真实执行。缺少部署配置和用户 GitHub 授权时保持 BLOCKED。
- [ ] B02 完整 VERIFIED 后，核对 B03 采用范围和前置条件，再按施工表开工。

## 吞吐检查点

- Blocking first steps. 先核实运行环境、指定阅读与最终证据。真实 App、固定 HTTPS 入口及服务端配置是 B02.6 运行前置。
- Independent workstreams. GPT-6 Astra 核查配置链路，gpt-5.6 sol 解析证据并在专属临时目录保存核对结果。主代理整理外部操作步骤和交接。
- Shared mutable state. 主代理是仓库唯一写入者。只读代理不编辑主树；证据执行代理只写其专属临时目录，主代理核对后导入新目录。旧 batch-02 证据与 .codex/config.toml 保持原字节。
- Smallest safe decomposition. 先交付可执行的配置和验收准备。本轮没有产品代码改动，不需要新增函数接口或重新比较产品架构。以后函数接口设计使用 GPT-6 Astra，实现使用 gpt-5.6 sol。

本轮不提交、不推送。真实验收状态不能由本地证据或配置说明的完成状态提升。
