# 选仓门(建项不选仓)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建 issue 不再选仓;① 分析后经"选仓门"(人工勾/AI 定)确认范围;③ 对称把关;接入项目时预建休眠团队。

**Architecture:** 四条互不重叠的工作流(A 契约+门端点 / B 发现链+automator / C 建队+唤醒 / D 前端),各占独立文件集,分 worktree 并行实施后顺序合并。

**Tech Stack:** Go(pgx,net/http)+ React/TS 前端 + AgentTeams REST。

**Spec:** `docs/superpowers/specs/2026-09-20-scope-after-requirement-design.md`(含全部文件:行号证据,实施前必读)。

## Global Constraints

- 迁移号 = 实施时 `internal/database/migrations/` 最大号+1;`TestEmbeddedMigrationsLoad` 必须绿。
- `canonicalize` 指纹算法**零改动**(repositoryIds 仍计入)。
- 门状态写入**不走 `discovery.save()`**,只用单列 CAS UPDATE。
- 每个 workstream 完成后:`go build ./...` 0 错、本流测试全绿、`go test ./...` 仅 observe 三包既有失败;D 另跑 `npx tsc -b` + `npx vite build` 0 错。
- 提交信息中文,风格对齐仓库现有(「加：/修：/接：」)。

---

### Task A1: options 端点去选仓化

**Files:** Modify `internal/issues/options.go:148-151,113-115`;Test `internal/web/issue_scope_test.go:119-128,153-155`
**Interfaces:** Produces `Options.CanSubmit bool` 新语义(配置+App 就绪即真);`PerRepositorySelectable` 不变(门用)。

- [ ] 写失败测试:无可用仓库(available=0)但配置就绪 → `CanSubmit=true`、BlockingReasons 不含 `NO_AVAILABLE_REPOSITORIES`
- [ ] 跑测试确认失败 → 改 `options.go`:`CanSubmit = configurationReady && appReady`(删 available>0 项);凭据观测全 unknown 时不再 503,压非阻塞 reason
- [ ] 更新 issue_scope_test.go 三处旧断言 → 测试绿 → commit `修：建项 options 不再要求可选仓库——选仓挪到门`

### Task A2: scope_gate 列 + 门状态 CAS 写

**Files:** Create `internal/database/migrations/00XX_scope_gate.sql`(号=max+1);Create `internal/discovery/gate.go` + `gate_test.go`;Modify `internal/discovery/service.go`(观测快照列清单补 scope_gate)
**Interfaces:** Produces:
```go
// internal/discovery/gate.go
type GateState string // "pending"|"resolved"
type Gate struct{ State GateState; DecidedBy string; Suggested []string; DeadlineAt *time.Time; ResolvedAt *time.Time }
func (s *Service) OpenGate(ctx, issueID string, suggested []string, deadline *time.Time) error // INSERT..ON CONFLICT DO NOTHING
func (s *Service) ResolveGate(ctx, issueID, decidedBy string) (bool, error) // UPDATE .. WHERE scope_gate->>'state'='pending' 返回是否翻转
func (s *Service) Gate(ctx, issueID string) (*Gate, error) // 读;空列返回 nil(老 issue)
```
- [ ] 迁移:`ALTER TABLE repomesh_issues.issue_discoveries ADD COLUMN IF NOT EXISTS scope_gate jsonb;`(注释写明为何新列:旧二进制 save() 整行重写会抹子键)
- [ ] 测试:OpenGate 幂等(重复开不覆盖);ResolveGate CAS(非 pending 返回 false);Gate 对无列值返回 nil → 实现 → 绿 → commit `加：选仓门状态列与 CAS 写(不走 save)`

### Task A3: 批量确认端点

**Files:** Create `internal/web/scope_selection.go` + `scope_selection_test.go`;Modify `internal/web/issue_queries.go`(挂路由)
**Interfaces:** Consumes A2 的 ResolveGate。Produces:
```
POST /api/projects/{pid}/issues/{iid}/scope/selection
{repositoryIds:[...](1..100,非空,去重), decidedBy:"manual"|"ai"|"timeout", idempotencyKey, expectedCreationContextRevision}
→200 {status:"committed", repositoryCount} ;422 空/越界 ;409 revision 不匹配或仓不在项目 ;幂等键重放 200
```
- [ ] 失败测试:空数组 422;revision 不匹配 409;成功后 `issue_repository_scope` 与 `issue_content_scope` **两表**同组、同一把 `scope_revision`、gate=resolved(仿 service.go:526-531 双表写,勿复用 AppendRepository)
- [ ] 实现 Handler(授权仿 issue_queries.go:24-36 的 project route;事务内:校验→双表写→ResolveGate)→ 绿 → commit `加：选仓门批量确认端点(双表同事务+revision+CAS)`

### Task B1: ① 后开门 + 建议落 gate

**Files:** Modify `internal/discovery/planning.go`(PlanningCandidates 产物应用处 :324 附近)
**Interfaces:** Consumes A2 OpenGate。Produces: candidates 落库且 gate 缺失时→`OpenGate(suggested=候选全名, deadline=hitl?nil:now+10m)`(hitl 判定读 issues.hitl_mode)
- [ ] 测试:ai 模式 deadline=+10m 且 suggested 非空;hitl 模式 deadline nil;重复应用不开第二次门 → 实现 → 绿 → commit `接：候选落库即开选仓门(ai 带截止)`

### Task B2: automator 识别门

**Files:** Modify `cmd/repomesh-coordinator/discovery_auto.go`(pendingIssues :218-262、switch :140-160、autohostStep :196-214)
**Interfaces:** Consumes A2 Gate/ResolveGate + roomnotice。
- [ ] pendingIssues SQL 增列 `gate_pending, gate_deadline`(读 scope_gate),WHERE 加 `AND NOT (scope_gate->>'state'='pending' AND scope_gate->>'deadline_at' IS NOT NULL AND now() < (scope_gate->>'deadline_at')::timestamptz)`
- [ ] switch 在 `!p.hasCandidates` 前加:`case p.gatePending: return false`(不经 done,不计数);`case p.gateExpired: ResolveGate(decidedBy=timeout)+roomnotice「10 分钟未选,已按 AI 建议代选」+落建议为范围(调 A3 服务层等价物)`;autohostStep 对应加 2 个编号(注释保持"与 switch 一一对应")
- [ ] 测试(SQL 级,仿现有 pendingIssues 测试风格):门 pending 未到期不入选;到期入选;resolved 后走原 !hasCandidates → 绿 → commit `接：自动托管识别选仓门——等待不空转,超时代选`

### Task B3: 查漏 + 分档不越范围

**Files:** Modify `internal/discovery/planning.go`(新 PlanningGapAudit kind + 产物解析)、`classify.go`(:100-120 分档构建处)
**Interfaces:** Produces `d.scope_gate->'audit' = {missing:[{repository,reason}]}`(经单列 UPDATE,不走 save)。
- [ ] classify:候选 items 里 repository ∉ 已确认范围 → **不进 required/maybe**,汇入 gap 桶;validateRepositories 仍守 required/maybe(此时必过)——测试:agent 圈 5 仓、确认 3 仓 → 分档只含 3 仓、gap 桶 2 仓,**不再 409**
- [ ] PlanningGapAudit:manual 确认后由 coordinator 派(仿 PlanningCandidates),产物 missing[] 落 gate.audit;空 missing → 自动过门进 ③;有 missing → 停,等人(「补上并继续」=A3 追加;「就这样」=单列 UPDATE audit_passed)
- [ ] commit `接：③ 只对确认范围分档,agent 多圈改走查漏提示`

### Task C1: AgentTeams 客户端(休眠建/唤醒)

**Files:** Modify `internal/agentteams/workers.go` + `matrix_test.go` 同目录新 `lifecycle_test.go`
**Interfaces:** Produces:
```go
func (c *Client) CreateWorkerSleeping(ctx, name string) ([]byte,int,error) // POST /workers {"name":..,"state":"Sleeping"}
func (c *Client) EnsureReady(ctx, name string) (phase string, err error)   // POST ensure-ready;GET status 合并
func (c *Client) Wake(ctx, name string) ([]byte,int,error)
```
- [ ] httpstub 测试:CreateWorkerSleeping 带 state 字段;EnsureReady 对 409/非 Sleeping 响应返回需要 wake 的判定 → 实现 → 绿 → commit `加：AgentTeams 休眠建队与唤醒客户端`

### Task C2: 接入建队( Sleeping ) + 双保险唤醒

**Files:** Modify `cmd/repomesh-web/main.go`(OnRepositoriesConfirmed 扩批量接入钩子)、`internal/repositoryteams/service.go`(EnsureForRepository 变体:建即 Sleeping,串行限速 2s/仓,失败 WARN 不阻断)
**Interfaces:** Consumes C1。
- [ ] 测试:批量 3 仓 → 3 队全 Sleeping(远端 stub 记录 state);1 仓失败不阻断其余 → 实现 → 绿
- [ ] 唤醒:A3 确认成功后 fire-and-forget 对入选仓 EnsureReady(失败 WARN);coordinator 派活前对目标仓 EnsureReady,失败写入 run 失败原因 → 测试 → commit `接：接入即建休眠队,选用时唤醒`

### Task D1: 前端接线

**Files:** Modify(优先级序)`frontend/src/pages/workbench/WorkbenchPage.tsx`(961,979 门禁,983,1240,1264,1310,1198 删选仓;驱动器 :299 改无条件 `if (discovery.step === 2) return;`)、`treeModel.ts:74-88`(choose 条件改 `analysis!=null && detail.repositories.length===0 && candidates===null && materialization===null`)、`FocusPanel.tsx:1311-1343`(choose 卡扩门:建议+理由+两按钮+deadline 倒计时只渲染后端值)、`api/issues.ts:105,122`(optional)、`api/contract.ts`(DiscoveryView 加 `scope_gate`)、`api/client.ts:146`、`api/rooms.ts:97,110`(加 `?? []`)
**Interfaces:** Consumes A3 端点、B 的 scope_gate 投影。
- [ ] tsc -b 0 错(rooms ?? [] 防白屏是硬验收)→ vite build 0 错 → commit `接：前端去建项选仓,选仓门接管范围确认`

## Self-Review

- 覆盖 spec §2(A2)、§3.1(A1)、§3.2(A3,B1,B2,B3)、§3.3(C1,C2)、§3.4(B2/C2 已含 roomnotice)、§4(D1)。✓
- 依赖方向:A2 ← B1/B2/A3;C 独立;D 依赖 A3/B 投影契约(契约已在 plan 写死,D 可先行按契约编码)。✓
- 合并顺序:A → B → C → D(文件集互斥,顺序合并应无冲突)。
