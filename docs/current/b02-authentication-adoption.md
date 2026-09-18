# B02 认证实施采用记录

2026-09-12，用户针对 [B02 最小实施包](../development/2026-09-12-b02-preflight/README.md)回复“确认，继续”。本记录采用该包列明的范围，替代前置核对中“待确认采用”的状态。

## 已采用范围

- [认证浏览器稿](authentication-browser-api-draft.md) RM-FIRST-ACCESS-r3 的 B02 范围及[总包 §4](first-batch-complete-review.md)的 A1 至 A7，包括字段、固定 Destination、期限、限流、Cookie 代次、原尝试恢复及持久发现续扫。
- [身份内部稿](backend-first-development-access-draft.md)中与上述认证对应的稳定账号、服务端会话、尝试记录、加密连接、刷新唯一责任及代次隔离。冲突与已被替代的描述以浏览器 r3 为准。
- [配置来源稿](backend-first-batch-sources-draft.md) S01、S02 的认证秘密子集，包括独立根文件、信封加密、用途和归属绑定、持久包装次数、自检、权限、根轮换与恢复。模型保存的业务入口仍归 B04。
- S06 的 B02 登录、元数据发现及用户读权与 App 能力分开观察。Issue 建项和实际仓库写入的动作矩阵仍在后续批次落实。

单一 App 安装范围外的受邀私仓可能缺席，coverage 仍为 partial，J1 完整发现目标保留。后续 conversation_message Destination 不开放。项目、Issue、只读会话及模型原操作的真实返回链路随所属批次接通，当前不得伪造目标有权。

本次不采用模型测试、预算、模型出站、执行配置导入及 P9 等后续候选，不整体提升来源稿或总包的采用状态。

## 实施与验收

B02 转为 IN_PROGRESS。先完成可本地验证的秘密基础、持久认证、HTTP 与登录页面、GitHub 适配及 coordinator 发现续扫，每个单元记录运行证据并独立复核。

用户已确认 GitHub App 尚未配置。真实 OAuth、App 安装权限及浏览器 GitHub 往返验收仍待真实配置。替身测试可以证明本地行为，不能将 B02 整体标记为 VERIFIED。施工结果见[本批记录](../development/2026-09-12-batch-02/README.md)。
