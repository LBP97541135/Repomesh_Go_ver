# 设计:建项不选仓 —— 需求分析后的"选仓门"(2026-09-20)

状态:已评审(三轮对抗性审查,含三个并行子代理报告)。
目标分支:`feat/scope-gate`(自 `lbp/main` 起)。

## 0. 一句话

建 issue 只交需求;① 需求分析后出现**选仓门**(「我自己勾」/「让 AI 定」),确认的集合就是本 issue 的仓库范围;③ 的把关人与②的选择方**互换**(人工勾→LLM 查漏,AI 勾→人审);团队在**仓库接入项目时**以休眠 worker 预建,选进范围时唤醒。

## 1. 产品语义(用户已拍板)

| 决策 | 结论 |
|---|---|
| 建项 repositoryIds | 完全去掉(解析层保留字段但**忽略其值**,指纹不变) |
| 候选池 | 本项目已接入的全部仓库(现状 `loadRepoPool` 两级取数已满足) |
| 自动托管(ai)下的门 | 门也出现,**10 分钟**无人点→自动「让 AI 定」,房间留记录 |
| 人工勾选后的③ | LLM 查漏:**只提示,人决定**补不补(「补上并继续」/「就这样」) |
| AI 勾选后的③ | 人审分档:ai 模式沿用自动批,hitl 模式人批 |
| 建队时机 | 仓库**接入项目**时建(单仓/批量都建),worker `state=Sleeping`(懒启动) |
| 唤醒 | 选进范围时 `ensure-ready`(Failed/Pending 兜底 `wake`);用完靠上游 auto-sleep |

## 2. 数据模型

- **新列**(迁移,号取当时最大+1):`issue_discoveries.scope_gate jsonb` —
  `{state: pending|resolved, decided_by: manual|ai|timeout, suggested:[...], deadline_at, resolved_at}`。
  `deadline_at` **只在 ai 模式**置(①建议落库时刻+10 分钟);hitl 模式无截止、门无限等待——
  SQL 的排除谓词因此只匹配"有截止且未到期",无截止的门由 switch 的等待分支兜住。
  空缺 = 老 issue,视为 resolved(跳过门)。
  ⚠️ 必须新列,不能塞现有 jsonb:旧二进制 `save()` 整行重写会无声抹掉子键(reopen.go 等整块赋值路径)。
- **门写入不走 `save()`**:确认(web)与超时(coordinator)都用**单列 UPDATE**(带 `WHERE state='pending'` 的 CAS),避免与发现链整行写互相丢更新。
- 观测快照 `jsonb_build_object` 列清单同步补 `scope_gate`。

## 3. 后端改动

### 3.1 建项(internal/issues、internal/web)
- `parse.go`:保持 `repositoryIds` 在 known 集、仍计入 `canonicalize` 指纹(**指纹算法零改动**,老客户端同键重放不受影响);值忽略(不入 scope)。
- `insertWorkScope` 空列表:现状已安全(零行、无错),不动。
- `issue-creation-options`:`canSubmit = configurationReady && appReady`;逐仓 selectable 投影保留(给门用);凭据级观测失败从 503 降级为非阻塞标记。同步改 `issue_scope_test.go` 三处断言。

### 3.2 选仓门(discovery + coordinator)
- ① 完成且 `scope_gate` 缺失/pending 时,由现有 PlanningCandidates 产出 AI 建议(池=项目全仓,现状),建议同时落 `scope_gate.suggested` 与 candidates 块。
- **批量确认端点**(新):`POST /api/projects/{pid}/issues/{iid}/scope/selection`
  body `{repositoryIds:[...], decided_by, idempotency_key, expected_creation_context_revision}`。
  一次事务:校验 ≥1 仓、都在项目内、revision 匹配 → 写 `issue_repository_scope` **和** `issue_content_scope`(双表,建项路径同款)→ 整组一把 `scope_revision` → `scope_gate` 置 resolved。
  ⚠️ 不复用单仓 AppendRepository(不原子/revision 碎裂/漏写 content_scope)。
- **automator**(`discovery_auto.go`):
  - `pendingIssues` SQL **直接排除** `scope_gate.state='pending' AND now()<deadline_at` 的 issue(连 tick 都不捡);
  - switch 在 `!p.hasCandidates` 之前加两支:门等待(若被捡到则 `return false`,**不经 done()**,不进熔断计数);门超时(CAS 置 `timeout`,幂等键 `autohost:{issue}:gate-timeout`,落一行"AI 代选"进房间);
  - `autohostStep` 编号与 switch 一一对应。
- **查漏(③前置,仅 decided_by=manual)**:新 planning kind `PlanningGapAudit`(org_leader,技能 cross-repo-planning 同源):输入=需求+已选集合+建议集合,产物=`{missing:[{repository,reason}]}`。有 missing→停门展示,人「补上并继续」=走 3.2 确认端点追加;「就这样」=CAS 记 `audit_passed`。decided_by=ai/timeout 不跑查漏,直接人审/自动批(按模式)。
- **③ 分档硬约束**:classification 只对**已确认范围**内的候选分档;agent 多圈的仓不进 required/maybe,进 `gap_audit` 提示桶(根治今天的 409)。

### 3.3 扫描建队(cmd/repomesh-web、repositoryteams、agentteams)
- 挂接点:仓库**接入项目**成功后(`OnRepositoriesConfirmed` 单仓 + 批量接入钩子),对尚无队的 (project, scan_id) 逐仓:建 worker ×2(leader+w-0001,`"state":"Sleeping"`)→ 建队 → 落库(0057 形状)。
- 限速:串行 + 每仓间隔(如 2s),失败记 WARN **不阻断接入**(收敛循环 5 分钟兜底补)。
- 客户端:`agentteams` 包加 `CreateWorkerSleeping`(带 state)、`EnsureReady`/`Wake`(先 ensure-ready,非 Sleeping/Stopped 响应则 wake)。
- 唤醒双保险:3.2 确认端点成功后 fire-and-forget 对入选仓 ensure;coordinator 派活前对目标仓 ensure 一次,失败如实进 run 的失败原因。
- 拆队:先移团队成员再删 worker(上游 409 语义)。

### 3.4 房间通知(roomnotice)
门开/人确认/超时代选/查漏结果 各投一条(幂等键 `gate:{issue}:{event}`),复用现有 Notifier。

## 4. 前端改动(按优先级)

1. `WorkbenchPage.tsx`:删选仓 UI 与 `!selectedRepos.length` 门禁(979);驱动器 299 行改**无条件** `step===2 return`;门 handlers 复用 677-737。
2. `treeModel.ts`:"choose" 态条件从绑 hitl 改为 `analysis!=null && 范围空 && candidates==null && materialization==null`(老 issue 自动跳过)。
3. `FocusPanel.tsx` GateStack:choose 卡扩成选仓门(建议列表+理由+两个按钮+超时倒计时文案,倒计时只渲染后端 `deadline_at`,前端零计时器)。
4. `api/issues.ts`:105 行 `repositoryIds?`;122 行缺省不发送。
5. `api/contract.ts`:DiscoveryView 加 `scope_gate`;`api/client.ts:146` optional;`api/rooms.ts:97/110` 加 `?? []`(否则白屏)。
6. 查漏卡:GateStack 内新增(gap_audit 读面),两个按钮走 3.2 端点。

## 5. 测试与验证

- Go:门状态机(等待/超时/确认 CAS)、批量端点(双表+revision+≥1 校验+幂等重放)、automator 排除谓词(SQL)、分类不越确认范围、CreateWorkerSleeping/EnsureReady(httpstub)。`TestEmbeddedMigrationsLoad` 守迁移号。
- 前端:tsc+build;手测清单:新建无仓 issue→①→门→勾/超时→③→(查漏)→④⑤;老 issue 打开跳门。
- 线上:e2e 脚本(仍发 repositoryIds)必须原样通过(指纹兼容的验收)。

## 6. 明确不做

- 不改上游 AgentTeams;不做跨项目候选池;不做 K8s 唤醒耗时适配(部署是 embedded/Docker,代码不写死后端类型即可);老 issue 不回填门。
