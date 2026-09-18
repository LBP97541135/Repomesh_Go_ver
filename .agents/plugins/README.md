# RepoMesh 的 Codex 项目插件

本项目安装 pstack 0.9.28，来源和固定提交见 [来源记录](../../plugins/pstack/repomesh-source.json)。这里的插件服务于开发者的 Codex 工作流，不是 RepoMesh 产品的运行时 Skill 模块。

- [marketplace.json](marketplace.json) 指向仓库内的 [插件目录](../../plugins/pstack/)。
- [项目配置模板](../../.codex/config.example.toml) 用于在此可信项目中注册本地 marketplace 并启用 `pstack@pstack-claude`。复制为本机 `.codex/config.toml` 后填写仓库绝对路径；该本机文件不纳入 Git。不通过全局 `codex plugin add` 启用，也不向用户级 `.agents/skills` 或 prompts 目录安装。移动仓库后需将其中 `marketplaces.pstack-claude.source` 改为新的仓库绝对路径。
- Codex 可能在用户缓存中保存插件副本；缓存位置不改变项目配置的启用范围。
- 在此项目的新任务中选择 `pstack:poteto-mode`，或输入“使用 pstack 的 poteto-mode 处理这个任务”。也可按名称选择 `pstack:tdd`、`pstack:technical-writing` 等技能。所有 54 个技能及其引用一并保留。
- 安装不添加全局常驻指令、不改模型配置。插件中的流程不得扩大用户授权；项目 AGENTS.md、当前任务范围和宿主工具的实际能力仍须遵循。

Windows 已验证该固定版本的安装/技能发现、Bun 类型检查、orch 基本读写、watch-pr 只读查询，以及 Git Bash 下的两个脚本。保持 LF；Bash 脚本使用 Git Bash。生成器和上游同步工具存在 Windows 路径/换行兼容问题，不能直接用于更新；多代理完整流程和 Codex 历史恢复未验收。

上游文件按固定提交原样保存；额外加入许可证、来源记录和局部 LF 属性。更新应重新取得明确版本并验证差异，不在使用时自动拉取 main。停用时将项目 `.codex/config.toml` 中该插件的 `enabled` 改为 `false`。

安装后已使用桌面应用配套 Codex 0.153.4 实测：本项目的技能发现接口返回 54 个已启用技能（包含 31 个工作流和 23 个内部原则技能）；项目外目录不加载 pstack，用户级配置文件哈希保持不变。181 个上游文件逐字节核对一致。详细结果见[安装验证](installation-verification.json)。
