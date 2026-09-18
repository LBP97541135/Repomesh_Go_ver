# 源码与采用范围核对

主仓工作目录 `/home/xubohan/projects/Repomesh_Go_ver`，分支main，HEAD `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`；已有大量未提交B02/B03修改，不能仅用HEAD表示本次阅读版本。[baseline.json](baseline.json)保存461个非秘密源码/文档的原始SHA-256；[source-evidence.json](source-evidence.json)另绑定旧worktree附件与上游关键文件。哈希绑定字节，不把未提交代码称为该提交的全部内容。

## 分工和核对结果

| 只读调研者 | 范围与输出 | Astra关键复核 |
| --- | --- | --- |
| b04_source_audit / Lovelace，Sol high | internal/projects/access/secrets/database/web、入口组装、B04旧候选/声明、B03验收。结论为调用链、字段/锁序冲突和未验证项，无文件写入。 | 亲自读取projects storage/fixed配置和默认解析、access主体/observation、secrets Seal/reserveWrap/Inspect、web free注册及Runtime组装；据此选D01—04。 |
| b06_contract_audit / Hilbert，Sol high | Issue创建、持久化、P9、DB/CB、C05/C06、B03当前实现。输出操作身份、scope和授权缺口，无文件写入。 | 核对唯一HTTP规范化/回执与当前数据库0004；P9未采用、Issue无durable rejected、DB07是B03回归；据此细化D06—08。 |
| agentteams_compat_audit / Faraday，Sol high | 锁定上游类型、共享Manager配置/授权、旧调查与后续证据，仅四兼容问题。无fetch/实验/文件写入。 | 读取ManagerSpec、reconcile顺序、provisioner空授权、Higress/Aliyun fallback、start脚本和CFG-05报告；结论见COMPATIBILITY。 |

模型名称与effort是本轮明确路由；不声称系统外遥测证明。主设计独占附件和专题编辑；独立审阅不参与这些产物编写，详见[复核记录](REVIEW.md)。

## 影响架构的现有事实

| 证据位置 | 本次工作树事实 | 设计影响 |
| --- | --- | --- |
| internal/projects/storage.go resolveProfile/lockCatalog，0004_projects.sql defaults | inherit沿default profile current_version；没有pinned_version。 | 新默认必须增加明确版本，旧null语义与原fixed不改。 |
| internal/projects/types.go fixedConfiguration、storage.go inspectProfile、service.go Get/Update | fixed内有profile/version/秘密引用；inspect带INTEGRATION_NOT_AVAILABLE，canCreateIssue=false；显式完整配置才重解，no-op用深比较。 | 不复制独立配置系统；专用应用要新有限ReplaceModel，不能走完整PATCH；B06另核完整材料与当前资格。 |
| internal/secrets/store.go及相关包装实现 | Seal独立预扣root次数，再独立插密文；Inspect共享availability锁，New检查所需root。 | caller tx InsertPrepared前置Prepare；不持业务锁预扣；root全缺不能许诺close冷启动可用。 |
| internal/access/project_access.go 67—95,200—231 | 主体锁binding/session/account；观察connection锁只检查参与时间，transaction_timestamp减60秒。 | 全局偏序延续；B06新增App和完整scope观察、最终clock_timestamp核时。 |
| internal/access/cleanup.go 的auth孤儿清理 | 认证目的/owner白名单。 | B04 model vault独立维护，不能把认证清理拓成全secret扫描。 |
| internal/web/server.go:31、projects.go | RunConfigured + registerProjects，没有web.Server对象。 | 声明沿free register组装，分批更新一个RunConfigured与caller。 |
| internal/access/deployment.go / destination.go | Runtime持同一pool/service；已有Destination种类但只有已实现resolver。 | 同Store注入模型；模型/Issue恢复补目的resolver，不接受自由return URL。 |
| docs/development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md | B03 INTEGRATED_LOCAL_VERIFIED，独立复核PASS无开放P0/P1/P2；businessReady=false。 | 不重跑、不改完成状态；后续集成必须保护DB07等回归。 |

## 原始候选与采用区分

旧B04附件在 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b04-design-01/`，本轮全读六份：backend-candidate-a、backend-interfaces-a、http-ui-b、frontend-b、backend-final、frontend-final。文件名final未表示采用；原handoff/NEXT中的实施指令也未被本轮恢复。所有六文件的实际路径和SHA-256在source-evidence.json。

根AGENTS/README/CONTEXT、current导航/HANDOFF/启动/计划/Astra准备/前后端交接、首批浏览器/持久化/project说明、ADR索引及0013/0016/0019、用户列出的所有模型来源/操作/Key/应用/创建/P9/恢复/项目契约审查均已阅读。判断按具体章节和替代范围：B02采用子集、B03原固定规则、F04与Issue P1沿用；模型M/S、P9、C05/C06新字段仍候选。README内历史“全部API未实现”等文字只作历史阶段事实，当前实现以源码及HANDOFF为准。

旧upstream调查eeaab643和517caff9实测分开。未启动服务/DB/浏览器/容器，未读取凭据或私有证据目录，未更新上游或重跑实验。剩余需要实测的条件在ACCEPTANCE和COMPATIBILITY，不能由静态阅读或声明解析证明。
