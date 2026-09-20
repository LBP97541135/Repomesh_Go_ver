package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// skillBridge queries the active (promoted or newest canary) SKILL.md content
// for a given agent role from the skill governance tables. It returns the
// skill ID, version and full content of the first bound skill for the role.
// Returns ("", "", "") when no binding exists — the caller then dispatches
// without a skill (honest degradation, not a fabricated prompt).
type skillBridge struct{ pool *pgxpool.Pool }

type skillRef struct {
	SkillID string
	Version string
	Content string
}

func newSkillBridge(pool *pgxpool.Pool) *skillBridge { return &skillBridge{pool: pool} }

// ForRole returns the active skill content for the role. Skills are resolved
// through agent_skill_bindings: if an agent of this role has an active binding
// to a skill version, we use its content. Otherwise we fall back to the
// preset skill for the role from the global seed.
func (sb *skillBridge) ForRole(ctx context.Context, role string) (skillRef, error) {
	var sr skillRef
	// 1. Try an explicit active binding.
	err := sb.pool.QueryRow(ctx, `
		SELECT s.name, v.version, v.content
		FROM public.agent_skill_bindings b
		JOIN public.skill_versions v ON v.id = b.version_id AND v.status IN ('promoted','canary')
		JOIN public.skills s ON s.id = v.skill_id
		JOIN public.agents a ON a.id::text = b.agent_id::text
		WHERE a.role = $1 AND b.active = true
		ORDER BY b.bound_at DESC LIMIT 1`, role).
		Scan(&sr.SkillID, &sr.Version, &sr.Content)
	if err == nil && strings.TrimSpace(sr.Content) != "" {
		return sr, nil
	}
	// 2. Fall back to the seed preset for this role (latest promoted v1.0.0).
	err = sb.pool.QueryRow(ctx, `
		SELECT s.name, v.version, v.content
		FROM public.skills s
		JOIN public.skill_versions v ON v.skill_id = s.id AND v.status = 'promoted'
		WHERE s.target_agent_role = $1 AND s.organization_id IS NULL
		ORDER BY v.updated_at DESC LIMIT 1`, role).
		Scan(&sr.SkillID, &sr.Version, &sr.Content)
	if err != nil || strings.TrimSpace(sr.Content) == "" {
		return skillRef{}, nil
	}
	return sr, nil
}

// ForAll returns all active skills for a role (for the batch assembly case).
func (sb *skillBridge) ForAll(ctx context.Context, role string) ([]skillRef, error) {
	rows, err := sb.pool.Query(ctx, `
		SELECT s.name, v.version, v.content
		FROM public.skills s
		JOIN public.skill_versions v ON v.skill_id = s.id AND v.status = 'promoted'
		WHERE s.target_agent_role = $1 AND s.organization_id IS NULL
		ORDER BY s.created_at`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []skillRef
	for rows.Next() {
		var sr skillRef
		if err := rows.Scan(&sr.SkillID, &sr.Version, &sr.Content); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// SkillPromptSection formats a skill's content for injection into an agent prompt.
func SkillPromptSection(skillID, content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 你的技能（")
	b.WriteString(skillID)
	b.WriteString("，技能库原文）\n\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n")
	return b.String()
}
