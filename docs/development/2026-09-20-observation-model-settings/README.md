# 观测模型统一设置：实施与验证

2026-09-20，继续在 `codex/main-worktree-20260920-120900`／`/home/xubohan/projects/Repomesh_Go_ver-main-20260920-120900` 工作。基线为 `86f63e53ed67f231c1abfbe03793d30114e93bf3`，变更尚未提交；没有替换其他分支或共享运行实例。

## 实际交付

主控制台「设置 → 模型与 API」（`#/settings/models`）新增 Jev／DeepSeek 直接配置表单：模型输入和可用模型候选、API Key 输入、保存、测试连接／读取模型列表、重新读取配置。原仓库分析／项目执行的供应商目录继续在同页下方，不改变其独立的加密存储与项目引用关系。

保存经过真实登录、管理员权限与 Origin／CSRF 校验，写入工作台实际使用的私有 `model.json`／`assistant.json`。Key 留空保留旧值，首次配置需提供；保存后清空输入，页面永不读回明文 Key。模型列表使用供应商真实 API，模型名允许手输；固定 Jev 版本可能不出现在别名列表中，列表检查不代表评分成功。

Web 只向配置的本机工作台转发固定模型设置路径，不接受用户指定任意 URL、不发送浏览器 cookie／授权头、不跟随重定向、不使用环境 HTTP 代理。默认目标是 18090；本轮 19091 工作台应在 Web 启动时设置 `REPOMESH_OBSERVE_WORKBENCH_URL=http://127.0.0.1:19091`。浏览器导航的公开工作台地址应与其保持一致。

平台未就绪不会再挡住模型设置页；启动向导增加「模型与 API 设置」入口。独立工作台设置页仍可维护同一份配置，并补 DeepSeek 连接测试和模型列表候选。

配置成功仅表示下次评分／分析将读取此配置，不自动触发付费推理，也不接通尚未实现的原生 DSH。此次没有新增第二套 Key 库或替换仓库分析／执行模型。

## 验证结果

| 检查 | 结果 |
| --- | --- |
| 隔离 PostgreSQL 全量 Go | **701 PASS、0 FAIL、4 SKIP**。跳过两种显式浏览器服务、显式 Jev 实时测试和 RootFileOwner；其中本次浏览器及 Jev另行执行。 |
| Go 构建、vet、web／observeui race | 通过。 |
| 产品前端 build、lint | 通过；保留既有 bundle 提示和两条 hook 警告。 |
| 配置／边界测试 | 真实工作台落盘、0600 权限、Key 不回传、空值保留、模型变更、重启读取、供应商拒绝、仅回环、拒绝重定向、401／403、Origin／CSRF 均通过。 |
| 浏览器实际配置 | 真实管理员会话及设置后端；两家的真实 Key 通过表单保存，真实模型列表返回；切换 Jev 模型并刷新后仍存在，恢复原模型，无明文 Key 回传；源码仅以表单状态持有草稿，未写入浏览器持久存储。 |
| 未就绪导航 | 实测 setup 接口 404，启动向导可见；点击设置链接后两种模型表单可见，并完成配置。 |
| API 文档 | 基线与当前均为 52 项既有失败，新增失败为 0。 |

浏览器账号、仓库目录是隔离夹具；会话、权限、CSRF、设置 API、工作台私有保存、模型列表请求均走真实实现，没有把保存端点或列表响应替换为假成功。浏览器没有向站外发请求；供应商模型列表由服务端请求。两次成功配置流程共进行四次非生成模型列表请求，没有在表单验收中运行 rubric 或聚类。最初浏览器暴露的启动向导阻挡问题已经修复；初次长驻测试进程在验证完成后触及 Go 默认 10 分钟限制，最终改用明确 5 分钟超时、4 分钟服务上限并主动关闭，测试服务退出 0。

## Jev 证据复核

继续按此前 TypeSafe skill 方法，把源码和已执行检查作为数据，调用 `jev-1.13.0` 作独立 Choice 判断。第一轮 6 项中，直接表单、实际落盘、刷新／空 Key、权限和真实模型列表五项获 supported，置信度 0.90—0.99；未就绪导航项为 supported／0.54，未达到固定 0.8 门槛。

补充实际 404、向导入口点击与可见表单记录后，仅核验该次观测导航事实，结果为 insufficient／0.37，仍不采纳。两次显式 Jev 验收测试都退出 1，原始结果保留，没有降低阈值或反复抽取到通过。浏览器的确定性检查通过，但本次不宣称 Jev 全项验收通过。见 [Jev 原始概率摘要](jev-review.json)。

## 证据与使用

私有证据根为 `/home/xubohan/.local/state/repomesh-observe-jev-20260920-120900`：

- `checks/settings-delivery-summary.json` 及对应 `settings-go-all.log`／build／vet／race 日志。
- `checks/settings-browser-final/summary.json`、`model-settings.png`、`server.log`：最终表单验证与测试服务正常关闭；此前过程记录在 `settings-browser/`。
- `checks/jev-settings-*`、`jev-settings-navigation-*`：证据投影、typed 原始响应和未全通过的测试日志。
- `checks/settings-api-doc-check.json`：API 文档基线对账。

密钥未进入仓库、日志或截图。测试恢复原模型与 Key，隔离浏览器服务和 PostgreSQL 已停止，独立工作台 19091 保持运行。主控制台界面须运行此 worktree 的新前端及新 Web 二进制后才会生效；共享业务实例未自动切换。配置/API说明见[本地工作台](../../current/local-observation-workbench.md)与[前端说明](../../../frontend/README.md#本地观测入口与模型用途)。
