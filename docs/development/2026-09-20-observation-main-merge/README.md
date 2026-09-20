# 观测增量与最新 main 合并检查

2026-09-20，按用户要求检查并推送 GitHub main。观测功能提交为 `431ddb6`；核对的远端 main 为 `35ea41ab863d6a9c31684aaeb9a1ddadc80af361`。只在本任务独立 worktree 处理合并，其他本地分支及其未提交内容没有切换或覆盖。

## 兼容处理

README、环境示例、HANDOFF 和主设置页共四处文本冲突。文档保留两边采用范围；主设置页同时保留项目级 TypeSafe 测试／代码审查、本机观测 Jev rubric、DeepSeek 分析三个用途，权限和 Key 存储各自独立。上游 0058／0059 迁移原样保留，本轮不新增数据库迁移。

恢复主线新审核视图需要的 StageHistory projectId，并修复合并后类型检查发现的两处主线缺口：planId／actorId 声明移到实际使用它们的 FocusPanelProps；数据库分支验证采用自身随机幂等键，不向发现步骤专用类型传入不支持的 dbv。没有绕过 TypeScript 检查。

## 验证

- 隔离 PostgreSQL 全量 Go：**729 PASS、0 FAIL、5 SKIP**。跳过两项显式浏览器服务、两个显式真实模型／Codex 测试和 RootFileOwner；本轮未重复付费推理。
- `go build ./...`、`go vet ./...` 通过；web、observeui、observepipe、typesafe 的 race 通过。
- frontend 类型与生产构建通过，lint 保留既有两条 hook 警告；web 类型、35 项测试及生产构建通过。
- 合并版浏览器确认三种模型用途同时存在、Key 输入为空、0 页面错误；使用隔离管理员账号和真实只读工作台配置，不发起模型推理。浏览器测试服务正常退出。
- 新配置和本地观测的此前真实保存／模型列表／评分证据沿用各自报告；Jev 对个别实施声明的低置信度仍保留，不用构建或合并状态改写成整份 Spec 全通过。

私有证据根：`/home/xubohan/.local/state/repomesh-observe-jev-20260920-120900/checks/`；本轮文件为 `merge-go-summary.json`、`merge-go-all.log`、`merge-race.log` 及 `merge-browser/`。构建和前端命令在本次会话执行。提交前核对差异、冲突标记及实际密钥泄漏。

## 合并与部署边界

远端 main 的既有 workflow 在 main 更新后自动部署 Web／coordinator／host-executor 和前端；保留原 workflow，不添加 PR 在生产自托管 runner 执行的入口。采用非强制更新，若 main 发生未纳入本地的前进，应拒绝并重新核对。

独立 `repomesh-observe` 的部署仍按本地工作台说明：目标机需先运行新版工作台，Web 的 `REPOMESH_OBSERVE_WORKBENCH_URL` 指向它。当前三进程自动部署流水线不会自动安装该独立管理服务；服务不存在时，观测设置如实返回不可用，不妨碍其他业务启动。不会把本机测试 Key 上传到 GitHub 或生产。

审核提交增加 expected_evidence_version 必填条件，部署后保留旧页面的浏览器需刷新；过期证据的决定返回冲突。真实 DSH、流式 TTFT 和真实跨仓效果实验仍按原范围待验。本报告说明已检查范围内可合并，不表示这些后续能力通过。
