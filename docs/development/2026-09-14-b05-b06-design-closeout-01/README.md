# B05/B06 五项设计收口 01

日期：2026-09-14。状态：DESIGN_CLOSEOUT_COMPLETE / PRODUCT_NOT_IMPLEMENTED / RUNTIME_NOT_RUN。基线为`main@43d8c2afab1022f991530747ccac0cc99bf0c294`，开始时工作区干净。本轮只修订设计文档和声明附件，没有修改产品Go、TypeScript、SQL migration或产品功能。

## 收口范围

| 项目 | 已固定的设计基线 | 未扩张的边界 |
| --- | --- | --- |
| 测试预览与handler | Preview同次观察outstanding、quota和handler；TEST_ALREADY_OUTSTANDING提供本人可读定位；Submit仍重查409，原key和原输入回执优先。handler缺失为TEST_HANDLER_UNAVAILABLE，读取失败为503 TEST_HANDLER_UNCONFIRMED。 | Preview不预留额度或槽位。RESULT_UNCONFIRMED不用于只读观察。maxUnresolved和其他候选数值未采用。 |
| unknown运维关闭 | coordinator本地受限inspect/close使用受控操作员的带外材料，绑定test、permit、不可复用sender和credential能力版本。关闭幂等并追加审计。 | 不新增服务、host权限、任意shell、浏览器关闭或提供商自动撤销协议。没有两类证据时保持unknown。 |
| 逐路径锁序与共同owner account边界 | 所有owner写先锁account且后段不回取。B04保存使用principal→slot→catalog→Provider；B03/B06项目路径保留project→operation→catalog/profile，B05分别沿所调既有用例。来源先排序owner accounts，再取catalog。owner由DDL保持不可变。 | 本轮没有声称发现或运行复现死锁，也没有修改或迁移B03/B04产品代码。 |
| schema2 execution | 共享ManifestCommon不含execution集合。schema2只有一个权威executionProfiles；schema1原Manifest和精确回执重放不变。 | 不升级旧回执，不补写v1版本，不实现导入器。 |
| 窗口scope | actor/project外键二选一，scope_id从选中外键生成；UTC日开始规范化后由PK(scope_kind,scope_id,start_utc)拒绝同日第二窗口。 | 只记录DDL设计，不新增产品migration或运行账本。 |

前端恢复设计继续扩展`web/src/modelRecovery.ts`的定位、存储和读取代次。各业务保留独立parser、reducer及重试规则；`web/src/projectRecovery.ts`继续负责B03项目操作。旧`SaveLocator`调用者在未来实现中同批迁移，通过类型检查后才能删除旧类型。声明附件没有改动产品前端。

## 状态与验证

B02仍为`PAUSED_BY_USER`的外部验收状态。B03和B04仍为`INTEGRATED_LOCAL_VERIFIED`，不变为整批业务VERIFIED。B05、B06产品仍未实施；B07、B09及其他后续批次不在本轮。模型测试成功仍不是应用模型或创建Issue的前置条件。`expectedCreationContextRevision`的精确输入语义未改变。

本轮新增的T10至T14只登记待执行验收。历史[独立设计复核](../2026-09-13-b04-b06-design-01/REVIEW.md)、`checks.json`和业务`NOT_RUN`状态保持原样，没有重写为成功。静态检查结果记录在[decisions.tsv](decisions.tsv)；本轮未启动服务、数据库、浏览器、容器或真实外部业务，也未运行产品测试。

最终独立只读复核通过，没有阻塞或非阻塞finding。独立检查以`.ts`临时输入完成TypeScript strict校验，Go声明附件共31个package section／452项declaration解析通过，12个变更Markdown文件中的173个本地链接及anchor零错误，`git diff --check`退出0（保留既有LF／CRLF提示），并确认变更只含13个受跟踪文档和2个新增证据文件。作者侧此前只针对`b05.go.txt`解析13个package片段；该数值与独立复核的全部声明附件统计口径不同。
