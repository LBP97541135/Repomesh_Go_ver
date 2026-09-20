package humancontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ───────────────── 项目监管策略草稿（policy-draft） ─────────────────
//
// 这张面补的是「没有任何一个用户被问过」那一段。控制台的流程是
// 「提需求 → 自动分析 → 建团+建档案 → 干活」，建团与建档案在物化那一瞬间同时
// 发生，中间没有一条缝能让人把监管策略插进去。于是 execution_mode 恒为默认值、
// required_checkpoints 恒为空数组 —— 人工审核台从上线起就没有生产者
//（线上实测 review_requests 0 行、agent_teams 唯一一行 required_checkpoints=[]）。
//
// 存储：public.project_policy_drafts（迁移 0040），一个项目一份，整份覆盖写。
//
// 刻意不写 public.agent_teams：那一行的 execution_mode 已经被 assembly 占用成
// 'leader'（internal/assembly/assemble.go 的 ensureTeam），读面 internal/agents
// 还在用它区分 'leader'/'server'。把 auto/supervised/manual_controlled 塞进同一列，
// 就是两套语义叠在一格里 —— 日后谁读错都不会有人发现。api-design.md §4.4 说
// agent_teams 吸收 topology_policy_drafts，那条并入的前提是列先分家；这里如实
// 记下这个偏差，而不是为了让表名对上而制造一个读不懂的列。

// policyCheckpoints 是契约里的六个检查点（前端 ProjectCheckpoint 逐字对应）。
var policyCheckpoints = []string{
	"repository_scope", "specification", "execution",
	"validation", "delivery", "exception_escalation",
}

var policyCheckpointSet = func() map[string]bool {
	set := map[string]bool{}
	for _, name := range policyCheckpoints {
		set[name] = true
	}
	return set
}()

var policyRoleSet = map[string]bool{
	"organization_supervisor": true,
	"project_supervisor":      true,
	"repository_supervisor":   true,
}

var policyCodeAccessSet = map[string]bool{"none": true, "read": true, "write": true}

var policyActionSet = map[string]bool{
	"view_decisions": true, "approve_checkpoint": true, "request_changes": true,
	"pause_project": true, "resume_project": true, "cancel_project": true,
	"edit_specification": true,
}

// ErrPolicyFrozen 表示该项目的策略已随首次物化定死（HTTP 409）。
//
// 为什么要有这条：前端卡片上写着「过了物化这一步，这个需求的监管策略就定死了」，
// 而在加这一条之前那句话只是界面上的说法 —— 后端没有任何东西阻止改。策略决定的是
// 「这个需求会停在哪几处、谁能批」，同一份需求跑起来之后中途改强度，会让已经在旧
// 强度下做过的决策无从解释。定死必须由存储层持有，不能只靠一句文案。
var ErrPolicyFrozen = errors.New("humancontrol: policy draft is frozen")

// PolicyViolation 是一条域不变量被违反（HTTP 422）。
//
// 文案照抄前端 api/humanControl.ts 里逐条列出的后端原文：界面的三档映射与这
// 些规则一一对应，这些 422 一条都不该被用户看见 —— 看见就说明界面漏了
// 一条约束，那时该修界面。所以这里只如实回原文，不翻译成更好听的话。
type PolicyViolation struct{ Message string }

func (v *PolicyViolation) Error() string { return v.Message }

// PolicyGrant 是一条人工授权（前端 HumanProjectGrantView 逐字对应）。
type PolicyGrant struct {
	HumanPrincipalID string   `json:"human_principal_id"`
	Role             string   `json:"role"`
	CodeAccess       string   `json:"code_access"`
	ControlActions   []string `json:"control_actions"`
	// nil = 该授权覆盖整个项目（只有 repository_supervisor 能给非 nil）。
	RepositoryID *string  `json:"repository_id"`
	PathPatterns []string `json:"path_patterns"`
}

// PolicyDraftView 是草稿回读体（字段名即前端类型字段名）。
type PolicyDraftView struct {
	ProjectID           string        `json:"project_id"`
	CreatedBy           string        `json:"created_by"`
	ExecutionMode       string        `json:"execution_mode"`
	RequiredCheckpoints []string      `json:"required_checkpoints"`
	HumanGrants         []PolicyGrant `json:"human_grants"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	// Frozen = 已随首次物化定死，改不动了。界面据此把「修改」换成只读——
	// 定死与否是存储层的事实（frozen_at），不是界面的判断。
	Frozen bool `json:"frozen"`
}

// PolicyDraftCommand 是整份覆盖写的入参。三个字段都允许缺省
// （后端默认 auto / 空 / 空），但前端一律全传 —— 省略字段等于让默认值悄悄
// 参与决定监管强度，而这批迁移要解决的正是「没人被问过就跑成全自动」。
type PolicyDraftCommand struct {
	ExecutionMode       string
	RequiredCheckpoints []string
	HumanGrants         []PolicyGrant
}

// ResolveProjectScope 把路径上的作用域 id 解析成调用者名下的项目 id。
//
// 为什么要这一步：契约 §0 说「project_id 就是 issue_id」，前端工作台也确实是
// 拿着 issue id 在调（useIssueFlowState → fetchProjectTopology(issueId)）。而草稿
// 表的主键是真正的项目 uuid —— 直接强转会得到 22P02，而不是一句能读懂的 404。
// 所以两种 id 都收：先按 issue id 认，认不到再按项目 id 认。
//
// 归属规则与项目读面一致：项目的 owner 必须是调用者，否则 404
// （不泄露「这个项目存在，只是不是你的」）。**管理员在这一条上不越权** ——
// 需要越权的调用方用 ResolveProjectScopeAs。
func (s *Service) ResolveProjectScope(ctx context.Context, actor, scopeID string) (string, error) {
	return s.resolveProjectScope(ctx, actor, false, scopeID)
}

// ResolveProjectScopeAs 同 ResolveProjectScope，但 admin=true 时允许管理员越过
// owner 判定（ADR-0022：人工决议在证据漂移时由组织管理员兜底）。
//
// 越权的是「**这一个**项目」，不是「所有项目」——解析结果仍然只有一个项目 id，
// 不会退化成一次全量放行。人工审核台的拍板与项目控制走这一条：审核台上管理员
// 本来就看得见全部待审，若拍板反而 404，那是「看得见按不动」。
func (s *Service) ResolveProjectScopeAs(ctx context.Context, actor string, admin bool, scopeID string) (string, error) {
	return s.resolveProjectScope(ctx, actor, admin, scopeID)
}

func (s *Service) resolveProjectScope(ctx context.Context, actor string, admin bool, scopeID string) (string, error) {
	if actor == "" || scopeID == "" {
		return "", pgx.ErrNoRows
	}
	var projectID string
	err := s.pool.QueryRow(ctx, `SELECT i.project_id FROM repomesh_issues.issues i
		 JOIN repomesh_projects.projects p ON p.id = i.project_id
		 WHERE i.id=$1 AND (p.owner=$2 OR $3)`, scopeID, actor, admin).Scan(&projectID)
	if err == nil {
		return projectID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("humancontrol: scope lookup: %w", err)
	}
	// 不是 issue id 就按项目 id 认。用文本比较（id::text=$1），
	// 否则垃圾输入会触发 uuid 的 22P02，把「不是你的」变成 500。
	err = s.pool.QueryRow(ctx, `SELECT id FROM repomesh_projects.projects
		 WHERE id::text=$1 AND (owner=$2 OR $3)`, scopeID, actor, admin).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pgx.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("humancontrol: scope lookup: %w", err)
	}
	return projectID, nil
}

// PolicyDraft 读一个项目的监管策略草稿；没设过返回 pgx.ErrNoRows（→404）。
//
// 刻意不返 200 空对象：「没人决定过」和「有人决定了不设任何卡点」是两件事，
// 而这批迁移存在的理由正是把前者从后者里分出来。前端据此渲染「未设定」而不是
// 「全自动」。
func (s *Service) PolicyDraft(ctx context.Context, projectID string) (PolicyDraftView, error) {
	var view PolicyDraftView
	var checkpoints, grants []byte
	err := s.pool.QueryRow(ctx, `SELECT project_id::text, created_by, execution_mode,
		 required_checkpoints, human_grants, created_at, updated_at, frozen_at IS NOT NULL
		 FROM public.project_policy_drafts WHERE project_id=$1::uuid`, projectID).
		Scan(&view.ProjectID, &view.CreatedBy, &view.ExecutionMode,
			&checkpoints, &grants, &view.CreatedAt, &view.UpdatedAt, &view.Frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyDraftView{}, pgx.ErrNoRows
	}
	if err != nil {
		return PolicyDraftView{}, fmt.Errorf("humancontrol: load policy draft: %w", err)
	}
	// 空集合一律回 []，不回 null：前端把这两个字段当集合遍历，null 会让
	// 「没有任何卡点」在界面上变成一次崩溃。
	view.RequiredCheckpoints = []string{}
	if len(checkpoints) > 0 {
		if err := json.Unmarshal(checkpoints, &view.RequiredCheckpoints); err != nil {
			return PolicyDraftView{}, fmt.Errorf("humancontrol: decode checkpoints: %w", err)
		}
	}
	view.HumanGrants = []PolicyGrant{}
	if len(grants) > 0 {
		if err := json.Unmarshal(grants, &view.HumanGrants); err != nil {
			return PolicyDraftView{}, fmt.Errorf("humancontrol: decode grants: %w", err)
		}
	}
	for index := range view.HumanGrants {
		if view.HumanGrants[index].ControlActions == nil {
			view.HumanGrants[index].ControlActions = []string{}
		}
		if view.HumanGrants[index].PathPatterns == nil {
			view.HumanGrants[index].PathPatterns = []string{}
		}
	}
	return view, nil
}

// PutPolicyDraft 整份覆盖写。没有幂等键：一个需求只有一份监管意图，
// 改主意就是替换它，没有第二份草稿可以被重复创建。
func (s *Service) PutPolicyDraft(ctx context.Context, actor, projectID string, command PolicyDraftCommand) (PolicyDraftView, error) {
	checkpoints := command.RequiredCheckpoints
	if checkpoints == nil {
		checkpoints = []string{}
	}
	grants := command.HumanGrants
	if grants == nil {
		grants = []PolicyGrant{}
	}
	for index := range grants {
		if grants[index].ControlActions == nil {
			grants[index].ControlActions = []string{}
		}
		if grants[index].PathPatterns == nil {
			grants[index].PathPatterns = []string{}
		}
		// 空串与 null 是同一件事（「不限仓库」）。前端下拉的「不选」可能发来
		// 空串，让它在库里变成 '' 会让那条单条规则判成「有仓库范围」。
		if grants[index].RepositoryID != nil {
			trimmed := strings.TrimSpace(*grants[index].RepositoryID)
			if trimmed == "" {
				grants[index].RepositoryID = nil
			} else {
				grants[index].RepositoryID = &trimmed
			}
		}
	}
	// 定死之后一律拒绝改写（409，不是 422 —— 输入本身没错，是时机过了）。
	// 放在校验之前：已定死的项目上，用户改哪个字段都是同一句回答。
	if frozen, err := s.PolicyFrozen(ctx, projectID); err != nil {
		return PolicyDraftView{}, err
	} else if frozen {
		return PolicyDraftView{}, ErrPolicyFrozen
	}
	if err := validatePolicyDraft(command.ExecutionMode, checkpoints, grants); err != nil {
		return PolicyDraftView{}, err
	}
	if err := s.checkGrantAccounts(ctx, grants); err != nil {
		return PolicyDraftView{}, err
	}
	checkpointJSON, err := json.Marshal(checkpoints)
	if err != nil {
		return PolicyDraftView{}, fmt.Errorf("humancontrol: encode checkpoints: %w", err)
	}
	grantJSON, err := json.Marshal(grants)
	if err != nil {
		return PolicyDraftView{}, fmt.Errorf("humancontrol: encode grants: %w", err)
	}
	// created_by 跟着最新一次写入走：它是「设这份策略的人」，不是「第一个设的人」
	// —— 换了人改强度而这一列还指着旧账号，出事时问不到该问的人。
	if _, err := s.pool.Exec(ctx, `INSERT INTO public.project_policy_drafts
		 (project_id, execution_mode, required_checkpoints, human_grants, created_by)
		 VALUES ($1::uuid,$2,$3::jsonb,$4::jsonb,$5)
		 ON CONFLICT (project_id) DO UPDATE SET
		   execution_mode=EXCLUDED.execution_mode,
		   required_checkpoints=EXCLUDED.required_checkpoints,
		   human_grants=EXCLUDED.human_grants,
		   created_by=EXCLUDED.created_by,
		   updated_at=now()`,
		projectID, command.ExecutionMode, string(checkpointJSON), string(grantJSON), actor); err != nil {
		return PolicyDraftView{}, fmt.Errorf("humancontrol: save policy draft: %w", err)
	}
	return s.PolicyDraft(ctx, projectID)
}

// DeletePolicyDraft 撤回草稿，回到「未设定」。
//
// 没得撤时返回 pgx.ErrNoRows（→404）而不是一句轻快的 204：以为自己撤掉了
// 一份其实早被别人撤掉的策略，与真的撤掉了是两件事。
func (s *Service) DeletePolicyDraft(ctx context.Context, projectID string) error {
	// 撤回同样受冻结约束：定死之后连「撤掉」都不该发生 —— 撤回等于把强度
	// 悄悄降回全自动，与改写是同一种后果。
	if frozen, err := s.PolicyFrozen(ctx, projectID); err != nil {
		return err
	} else if frozen {
		return ErrPolicyFrozen
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM public.project_policy_drafts WHERE project_id=$1::uuid`, projectID)
	if err != nil {
		return fmt.Errorf("humancontrol: delete policy draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// checkGrantAccounts 确认每条授权指向的账号真的存在。
//
// 前端契约把这一条列成 422 human grant account does not exist：授权引用一个
// 不存在的账号，等于把审核权交给一个没人能登录的身份 —— 那种草稿存下去，
// 卡点触发时会挂在那里谁也不知道该找谁。
func (s *Service) checkGrantAccounts(ctx context.Context, grants []PolicyGrant) error {
	unique := map[string]bool{}
	for _, grant := range grants {
		if grant.HumanPrincipalID != "" {
			unique[grant.HumanPrincipalID] = true
		}
	}
	if len(unique) == 0 {
		return nil
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	var found int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_access.accounts WHERE id = ANY($1)`, ids).Scan(&found); err != nil {
		return fmt.Errorf("humancontrol: grant account lookup: %w", err)
	}
	if found != len(ids) {
		return &PolicyViolation{Message: "human grant account does not exist"}
	}
	return nil
}

// validatePolicyDraft 逐条判域不变量（设计文档 §4.8 的三档映射）。
//
// 三档不是「设一下更严一点」，而是三种形状：
//   - auto：卡点必须为空（auto 带卡点会被拒），一个人工卡点都不会有；
//   - supervised：至少一个卡点 + 至少一条授权，两者缺一都不收；
//   - manual_controlled：卡点必须是全部六个，少一个就拒。
func validatePolicyDraft(executionMode string, checkpoints []string, grants []PolicyGrant) error {
	switch executionMode {
	case "auto", "supervised", "manual_controlled":
	default:
		return &PolicyViolation{Message: fmt.Sprintf("unknown execution mode %q", executionMode)}
	}
	present := map[string]bool{}
	for _, checkpoint := range checkpoints {
		if !policyCheckpointSet[checkpoint] {
			return &PolicyViolation{Message: fmt.Sprintf("unknown checkpoint %q", checkpoint)}
		}
		present[checkpoint] = true
	}
	switch executionMode {
	case "auto":
		if len(present) > 0 {
			return &PolicyViolation{Message: "automatic projects cannot require human checkpoints"}
		}
	case "supervised":
		if len(present) == 0 {
			return &PolicyViolation{Message: "human-controlled projects require checkpoints"}
		}
	case "manual_controlled":
		if len(present) != len(policyCheckpoints) {
			return &PolicyViolation{Message: "manual-controlled projects require every human checkpoint"}
		}
	}
	// 非 auto 一律要有授权：一个会停下来的项目却没有能批它的人，就是一条
	// 永远走不完的流水线。
	if executionMode != "auto" && len(grants) == 0 {
		return &PolicyViolation{Message: "human-controlled projects require a human grant"}
	}
	seen := map[string]bool{}
	for _, grant := range grants {
		if grant.HumanPrincipalID == "" {
			return &PolicyViolation{Message: "human grant requires an account"}
		}
		if !policyRoleSet[grant.Role] {
			return &PolicyViolation{Message: fmt.Sprintf("unknown human project role %q", grant.Role)}
		}
		if !policyCodeAccessSet[grant.CodeAccess] {
			return &PolicyViolation{Message: fmt.Sprintf("unknown code access level %q", grant.CodeAccess)}
		}
		if len(grant.ControlActions) == 0 {
			return &PolicyViolation{Message: "human grant requires control actions"}
		}
		for _, action := range grant.ControlActions {
			if !policyActionSet[action] {
				return &PolicyViolation{Message: fmt.Sprintf("unknown control action %q", action)}
			}
		}
		repository := ""
		if grant.RepositoryID != nil {
			repository = *grant.RepositoryID
		}
		// 四条单条规则（前端 api/humanControl.ts 逐条列过）：
		// 1) repository_supervisor 必须给 repository_id；
		// 2) 另两种身份不许给；
		// 3) 有 path_patterns 就必须先有 repository_id；
		// 4) control_actions 至少一个（上面已判）。
		if grant.Role == "repository_supervisor" && repository == "" {
			return &PolicyViolation{Message: "repository supervisor requires repository scope"}
		}
		if grant.Role != "repository_supervisor" && repository != "" {
			return &PolicyViolation{Message: "only a repository supervisor may carry a repository scope"}
		}
		if len(grant.PathPatterns) > 0 && repository == "" {
			return &PolicyViolation{Message: "path patterns require a repository scope"}
		}
		scope := grant.HumanPrincipalID + "::" + grant.Role + "::" + repository
		if seen[scope] {
			return &PolicyViolation{Message: "duplicate human grant scope"}
		}
		seen[scope] = true
	}
	return nil
}

// RequiredCheckpointsFor 读一个项目当前要求的卡点集合（物化/执行面用）。
//
// 没设过草稿时返回**空集合而不是报错**：没设过 = 还没人决定过，执行面的默认
// 形态是全自动（与前端「未设定」那句陈述同源），不是一次失败。
func (s *Service) RequiredCheckpointsFor(ctx context.Context, projectID string) ([]string, error) {
	view, err := s.PolicyDraft(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return view.RequiredCheckpoints, nil
}

// RequiresCheckpoint 报告某个卡点是否要求人工。auto 恒为 false。
func (s *Service) RequiresCheckpoint(ctx context.Context, projectID, checkpoint string) (bool, error) {
	view, err := s.PolicyDraft(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if view.ExecutionMode == "auto" {
		return false, nil
	}
	for _, name := range view.RequiredCheckpoints {
		if name == checkpoint {
			return true, nil
		}
	}
	return false, nil
}

// FreezePolicyDraft 在首次物化时盖章：此后这份策略改不动了。
//
// 幂等：已盖章的项目再调一次什么都不做（物化本身可重放）。
// **没设过草稿时也盖章**吗？不 —— 没有草稿就没有东西可定死，此时不插行，
// 让「未设定」这条事实保持原样（前端据此渲染「未设定」而不是「全自动」）。
func (s *Service) FreezePolicyDraft(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE public.project_policy_drafts
		 SET frozen_at=now() WHERE project_id=$1::uuid AND frozen_at IS NULL`, projectID); err != nil {
		return fmt.Errorf("humancontrol: freeze policy draft: %w", err)
	}
	return nil
}

// PolicyFrozen 报告该项目是否已定死（前端据此禁用「修改」并给出理由）。
func (s *Service) PolicyFrozen(ctx context.Context, projectID string) (bool, error) {
	var frozen bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.project_policy_drafts
		 WHERE project_id=$1::uuid AND frozen_at IS NOT NULL)`, projectID).Scan(&frozen)
	if err != nil {
		return false, fmt.Errorf("humancontrol: policy frozen lookup: %w", err)
	}
	return frozen, nil
}
