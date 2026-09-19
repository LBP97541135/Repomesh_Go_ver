# 独立验收器与评估结果复核

日期：2026-09-19。源码基线：`28c31677e349cd89c5abff3c7cf26e8b960d526a` 加本轮工作区修改。复核对象为 [evaluation.go](../../../internal/observepipe/evaluation.go)、[dataset.go](../../../internal/observepipe/dataset.go)、对应测试与 [evals 资产](../../../evals/README.md)。复核者不是上述实现者；以下问题由复核者独立复现并反馈，原实现者修复后再独立复验。

结论：发现的两项 P2 问题已修复，针对性验收通过。本地固定产物行为、三态聚合和平台结果文件转换在已测范围内成立。没有据此确认真实 AgentLoop 云端评估、DSH 能力、服务部署证明或交付成功率改善。

## 发现、修复与独立复验

| 编号 | 修复前的独立证据 | 已复核的修复 |
| --- | --- | --- |
| E1／P2 | 订单接口返回合法 JSON `{"order_id":"record-1","amount_cents":2000,"contract_version":42}`。2000 分已经违反固定的 8000 分断言，但整体结构反序列化错误令所有相关检查变成 unknown，总结果也为 unknown。 | 报告字段独立解析；错误字段不抹去其他有效字段。已知金额不符保留 fail，非法金额不转换为 0。组合契约检查中，已知原价／比例错误也优先保留 fail；只有无已知错误且证据不全时才为 unknown。 |
| E2／P2 | 文本诊断的 `custom_outputs={"verdict":"pass","evidence_refs":[""]}` 被转换成 `scored/pass`，空字符串绕过了非空证据检查。 | 归一化时去除空白引用；全部为空时转换为 `unknown/unknown`，不生成分数。原始平台记录仍保留，混合有效引用时保留规范化后的有效引用。 |

E1 的独立测试在修复前输出：

```text
verdict=unknown grading_status=error
order.amount: Status=error Verdict=unknown Value=nil
FAIL: known amount failure was erased by malformed independent contract field
```

修复后相同响应输出 `verdict=fail`、`grading_status=error`，`order.amount` 为 `scored/fail`，`Expected=8000`、`Actual=2000`。失败与其他检查的证据错误同时保留。

E2 的独立测试在修复前输出 `verdict=pass status=scored evidence=[""]`；修复后为 `verdict=unknown status=unknown evidence=[]`。

## 验收覆盖

| 检查 | 实际观察 |
| --- | --- |
| 固定产物正反例 | baseline 的返回及持久化金额均为 2000，判 fail；candidate 为 8000，判 pass；装配错误判 unknown，身份不匹配后不运行目标组合的金额检查。 |
| 无效的成功声明 | HTTP 成功、空对象、破损 JSON、零测试的自报成功、响应内 `verdict=pass` 均不能替代金额断言。 |
| 已知失败与缺证 | 缺少独立检查不抹去已知失败；新增 9 个子例覆盖字段顺序、错误契约类型、错误身份字段、价格报告兄弟字段、复合契约已知错误及读回错误订单身份。 |
| 非法数字 | 字符串、布尔、浮点字面量、指数形式、超出 int64 上下界、null、数组和对象共 10 类仍为 unknown，`value=null`；无整数截断、舍入或默认 0。 |
| 三态聚合 | 没有必检项为 unknown/pending；缺必检项、必检 not_applicable、相互冲突的未选定评分不能判 pass；可选评分错误不会单独推翻完整必检通过。 |
| 不同 Trial | 合并必需评分时，缺 Trial 或来自不同 Trial 返回 unknown/error；不按 session 或时间范围混合试验。 |
| 平台状态与业务结果 | 平台 `status=success` 且评分为 0 仍为业务 fail；平台 failed／unknown 即使带评分 1 也不判 pass。未知状态、错误字段名与非法评分范围不被猜测转换。 |
| 冻结 grader 规则 | 必需性、评分方向、使用原始或归一化字段及阈值来自本地固定配置，不采纳被评记录自行声明的规则。缺所选分数字段不回退到另一个字段。 |
| 精确来源 | 核对平台 task／run／evaluator 与 dataset item 或 subject Trace 的绑定；被评 Trace 与 Judge 自身 Trace 分开保留。 |
| CSV 可复核性 | 一 Trial 一行；重复 Trial 在写入前拒绝；中文、逗号、引号、换行和嵌套 JSON 往返保留，写入错误不会被忽略。 |

CSV 包含公开任务、冻结清单、完整 HTTP 验收报告、产物身份、真实 rubric 文本、Trace／证据引用与三个不同的状态字段；用于本轮本地验收与诊断输入是充分的。`relevant_trajectory` 明确列出缺少选仓、规划、Agent 执行和人工审查；`external_evidence_access` 为未配置，没有把仅有 hash 的引用当成云端已可读取证据。

## 资产及官方字段核对

逐份解析了 7 个 JSON 资产，核对 Prompt 的 7 个变量与 `variable-mapping.json` 及 CSV 列对应。`case.json` 的 8 项必检顺序与代码一致，预期价格、返回订单及读回金额均为 8000 分。数值结果样例带 `fixture_only=true`；契约交接 grader 为非必需诊断，不承担独立行为验收。

本次另行读取了[官方评估结果字段说明](https://help.aliyun.com/zh/agentloop/developer-reference/field-descriptions-for-evaluation-results)，核对 `status`、`result_type`、原始／归一化分数、`data_link`、`eval_meta` 和自定义输出的角色。当前代码使用其已列出的字段，平台执行状态与业务判定保持分离。官方字段说明不证明本机完成了数据集导入或评分执行；本地 CSV／JSON 仍是 RepoMesh 的适配格式。

## 执行记录

复核开始时执行现有相关测试，16 个顶层测试通过，但 E1／E2 两个独立反例均失败。反例通过临时 Go overlay 加载，没有修改被复核实现。

修复后执行同一组独立反例及新增的仓库测试：

```bash
go test -overlay "$REVIEW_OVERLAY" ./internal/observepipe -count=1 -v \
  -run 'Test(ReviewMalformed|ReviewEmpty|Discount|Verifier|Assembly|Aggregate|Manifest|TrialCSV|Platform|Merge)'
git diff --check -- internal/observepipe/evaluation.go internal/observepipe/dataset.go \
  internal/observepipe/evaluation_test.go internal/observepipe/dataset_test.go
```

`REVIEW_OVERLAY` 为本次临时反例映射。修复后共 20 个顶层测试通过、0 跳过，其中 18 个已保存在仓库、2 个为本次独立反例；包用时 0.175s。两个反例的永久覆盖已进入 [evaluation_test.go](../../../internal/observepipe/evaluation_test.go) 和 [dataset_test.go](../../../internal/observepipe/dataset_test.go)。格式检查通过；JSON 资产、变量映射、Oracle 和必检表检查通过。

测试只使用本机临时 HTTP 服务、临时文件和内存结果，没有调用模型、云端评分器或共享产品服务。全量工程检查由主批次另行记录，本报告不代为宣称通过。

## 仍需保留的边界

- `/version` 与订单响应中的价格身份是配置端点的声明。当前工具没有通用部署证明、测试期间的版本锁定或生产数据库直连证明；对外部署的使用者必须从可信控制面补齐这些依据。固定夹具中的可执行文件摘要和 `synthetic:` 标签已明确区分。
- 已过滤空引用，但平台结果归一化器不解析任意诊断引用的实际可达性，也不判断引用内容是否支持结论。真正的 Judge 接入仍需 Trial 范围内取证及人工校准；保存诊断结果不等于确认根因。原始独立验收不会被该诊断覆盖。
- CSV 中确实内联了本轮 HTTP 证据，但没有 DSH 实际消费契约的轨迹。契约诊断不能仅凭金额失败断言漏仓或契约未同步，当前 Prompt 已要求此时返回 unknown。
- 固定三种服务分支只校验验收器。本轮没有真实 Agent 生成改动、同起点的 Agent 重跑、留出集或泛化对照；不报告团队能力提升。
