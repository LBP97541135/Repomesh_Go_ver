package skill

import (
	"context"
	"net/http"
)

// Service is the lifecycle facade the API layer talks to. It owns the state
// machine plus both gates, mirroring the Python SkillRegistryService.
type Service struct {
	Store *Store

	// Authenticate guards every mounted endpoint; nil skips the check.
	Authenticate func(r *http.Request) error
	// ActorName resolves the acting principal for audit fields.
	ActorName func(r *http.Request) string
	// ActorOrganization resolves the acting principal's space (organization id).
	//
	// 2026-09-19 账号隔离：技能库此前是**全局一份**（迁移 0039 前 skills 表没有
	// 空间归属），公有部署（一账号一空间）下任何登录账号都能看到、并按名字覆盖
	// 别人的技能。未注入该 seam 时返回空串 —— 那时只认「全局种子技能」，
	// 宁可少给，也不越权多给。
	ActorOrganization func(r *http.Request) string

	// Judge 是 A/B 评估的模型侧（两臂作答 + 盲裁判）。
	//
	// 2026-09-21：此前 A/B 判定是"拿技能正文去比对测试题关键词"—— 对照组恒为
	// 空串，结论恒为 win，等于没有判定。现在换成真盲评。**未注入时为 nil，
	// 此时 RunABEvaluation 直接拒绝并如实说明**，绝不退回假判定。
	Judge ABJudge
}

func NewService(store *Store) *Service { return &Service{Store: store} }

// organization 取调用者的空间标识；未注入 seam 时为空串（只认全局种子技能）。
func (s *Service) organization(r *http.Request) string {
	if s.ActorOrganization != nil {
		return s.ActorOrganization(r)
	}
	return ""
}

func (svc *Service) RegisterVersion(ctx context.Context, organizationID, skillName, version, content, createdBy string) (*SkillVersion, error) {
	if !ValidSemver(version) {
		return nil, Refused("skill_version_invalid", "version %q is not semantic (MAJOR.MINOR.PATCH)", version)
	}
	sk, err := svc.Store.GetSkillByName(ctx, organizationID, skillName)
	if err != nil {
		// 调用方给的可能是 **id**（控制台就是：路由体里的字段叫 skill_id，前端自然
		// 传 uuid）—— 上面按**名字**查必然 not_found，于是整条"登记新版本 → 送评估"
		// 永远走不到（2026-09-20 实测：POST /api/skills/versions 固定 409，
		// evaluation_runs 至今 0 行）。这里再按 id 查一次，**同样过组织作用域**。
		byID, idErr := svc.Store.getSkillByIDScoped(ctx, organizationID, skillName)
		if idErr != nil {
			return nil, Refused("skill_not_found", "skill %q is not registered", skillName)
		}
		sk = byID
	}
	return svc.Store.RegisterVersion(ctx, sk.ID, version, content, createdBy)
}

func (svc *Service) Transition(ctx context.Context, versionID string, to Status) (*SkillVersion, error) {
	v, err := svc.Store.GetVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if err := AssertTransition(v.Status, to); err != nil {
		return nil, err
	}
	return svc.Store.Transition(ctx, versionID, to)
}

func (svc *Service) StartEvaluation(ctx context.Context, versionID string) (*SkillVersion, error) {
	return svc.Transition(ctx, versionID, StatusEvaluating)
}

// EnterCanary enforces the clean gate: across the skill's whole history there
// must be at least one pass and zero fails.
func (svc *Service) EnterCanary(ctx context.Context, versionID string) (*SkillVersion, error) {
	v, err := svc.Store.GetVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	ok, err := svc.Store.CleanGateOK(ctx, v.SkillID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, Refused("skill_gate_failed",
			"clean gate not satisfied for skill %s: need at least one pass and zero fails in history", v.SkillID)
	}
	return svc.Transition(ctx, versionID, StatusCanary)
}

// Promote enforces the canary-window gate: runs recorded after the version
// entered canary need at least one pass and zero fails.
func (svc *Service) Promote(ctx context.Context, versionID string) (*SkillVersion, error) {
	ok, err := svc.Store.CanaryWindowOK(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, Refused("skill_gate_failed",
			"canary window gate not satisfied for version %s: need at least one pass and zero fails since canary started", versionID)
	}
	return svc.Transition(ctx, versionID, StatusPromoted)
}

func (svc *Service) Rollback(ctx context.Context, versionID string) (*SkillVersion, error) {
	return svc.Transition(ctx, versionID, StatusRolledBack)
}

func (svc *Service) ResolveCurrent(ctx context.Context, organizationID, skillName string) (*SkillVersion, error) {
	sk, err := svc.Store.GetSkillByName(ctx, organizationID, skillName)
	if err != nil {
		return nil, Refused("skill_not_found", "skill %q is not registered", skillName)
	}
	return svc.Store.ResolveCurrent(ctx, sk.ID)
}

// RecordRun delegates to the store, which enforces evaluating/canary-only and
// the canary auto-rollback on fail.
func (svc *Service) RecordRun(ctx context.Context, versionID, questionID, arm, blindedLabel string,
	answer map[string]any, judgedBy *string, result string) (*EvalRun, error) {
	switch result {
	case ResultPass, ResultFail:
	default:
		return nil, Refused("skill_evaluation_refused", "result must be pass or fail, got %q", result)
	}
	switch arm {
	case ArmWith, ArmWithout:
	default:
		return nil, Refused("skill_evaluation_refused", "arm must be with or without, got %q", arm)
	}
	return svc.Store.RecordRun(ctx, versionID, questionID, arm, blindedLabel, answer, judgedBy, result)
}

// ReleaseOrAutoPass is the composition of tiered approval + revision auto-release:
//   - a version with an approved tiered review is bound via approval_release;
//   - a small revision (token similarity >= 0.7 vs the promoted version) skips
//     re-review and binds via revision_auto;
//   - a major revision (>30% changed) must go through full review.
func (svc *Service) ReleaseOrAutoPass(ctx context.Context, versionID string, agentID string) (string, error) {
	v, err := svc.Store.GetVersion(ctx, versionID)
	if err != nil {
		return "", err
	}
	sk, err := svc.Store.getSkillByID(ctx, v.SkillID)
	if err != nil {
		return "", err
	}
	ap, err := svc.Store.GetApproval(ctx, versionID)
	if err != nil || ap.ReviewStatus != "approved" {
		// No approved review yet — check the small-revision auto pass.
		current, _ := svc.Store.ResolveCurrent(ctx, sk.ID)
		if current != nil && TokenSimilarity(current.Content, v.Content) >= MinAutoReleaseSimilarity {
			if err := svc.Store.BindAgent(ctx, agentID, versionID, BindingRevisionAuto); err != nil {
				return "", err
			}
			return BindingRevisionAuto, nil
		}
		return "", Refused("skill_approval_required",
			"version %s needs tiered review (%s) before release", versionID, string(ReviewerFor(sk.TargetAgentRole)))
	}
	if err := svc.Store.BindAgent(ctx, agentID, versionID, BindingApprovalRelease); err != nil {
		return "", err
	}
	return BindingApprovalRelease, nil
}

// AssembleWithVersions resolves each preset skill ID to its currently active
// version and returns a dispatch-ready capability bundle with version numbers
// and content hashes. This is what the coordinator calls before dispatching a
// task: the resulting version set is what the task runs against.
type ResolvedEntry struct {
	SkillID     string `json:"skill_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
}

type ResolvedBundle struct {
	Role    string          `json:"role"`
	Profile string          `json:"profile"`
	Skills  []ResolvedEntry `json:"skills"`
	Servers []McpPolicy     `json:"servers"`
}

func (svc *Service) AssembleWithVersions(ctx context.Context, organizationID, role, profile string, taskFeatures []string) (*ResolvedBundle, error) {
	base, err := Assemble(role, profile, taskFeatures)
	if err != nil {
		return nil, err
	}
	out := &ResolvedBundle{Role: base.Role, Profile: profile}
	if out.Profile == "" {
		out.Profile = DefaultTeamProfile
	}
	for _, skillID := range base.Skills {
		v, err := svc.Store.ResolveCurrent(ctx, skillID)
		if err != nil {
			return nil, Refused("skill_no_active_version",
				"skill %q has no promoted or canary version; refuse to dispatch without it", skillID)
		}
		out.Skills = append(out.Skills, ResolvedEntry{
			SkillID:     skillID,
			Version:     v.Version,
			ContentHash: v.ContentHash,
			Content:     v.Content,
		})
	}
	if out.Skills == nil {
		out.Skills = []ResolvedEntry{}
	}
	policies, err := svc.Store.ListMcpPolicies(ctx)
	if err != nil {
		return nil, err
	}
	features := map[string]bool{}
	for _, f := range taskFeatures {
		features[f] = true
	}
	for _, p := range policies {
		if len(p.RequiredTaskFeatures) > 0 {
			ok := false
			for _, rf := range p.RequiredTaskFeatures {
				if features[rf] {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		out.Servers = append(out.Servers, p)
	}
	if out.Servers == nil {
		out.Servers = []McpPolicy{}
	}
	return out, nil
}
