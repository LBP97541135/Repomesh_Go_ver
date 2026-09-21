// repomesh-skill-ab：给**全部 skill** 跑一遍 A/B 盲评并留存记录。
//
// 为什么需要这个程序：A/B 入口是 HTTP 端点（要会话 + CSRF），而"每个预设 skill 都有一条
// 真实评估记录"这件事只需要跑一次、留档即可。本程序走的是**与端点完全相同的服务方法**
// （skills.Service.RunABEvaluation）—— 判定只有一份（internal/skills/abeval.go），
// 这里**不另写一套**，否则两套判定迟早分叉。
//
// 2026-09-21：判定已换成**真盲评**（两臂各用模型作答 → 盲裁判选优 → 反盲落库）。
// 所以本程序**必须**有可用的模型：环境变量 REPOMESH_AB_BASE_URL / REPOMESH_AB_API_KEY /
// REPOMESH_AB_MODEL，或数据库里一条启用的中转站 + REPOMESH_SECRET_ROOT（解封密钥）。
// 取不到就**直接退出**，不写任何记录 —— 宁可没有记录，也不要假记录。
//
// 前置约束（线上实测）：执行器只接受 **evaluating / canary** 状态的版本。
// 线上 15 个 skill 里 14 个只有 `1.0.0:promoted`，所以这里先给它们各建一个
// `1.1.0:evaluating` 候选版本 —— **正文与 content_hash 原样复制 1.0.0**
// （方案 A，用户 2026-09-21 裁定：拿现有正文当候选跑一次基线 A/B，不编任何内容）。
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/skills"
)

// onlySkills 非空时只跑这些 skill 名（逗号分隔），用于小步验证。
func main() {
	dbURL := os.Getenv("REPOMESH_DATABASE_URL")
	if dbURL == "" {
		fmt.Println("REPOMESH_DATABASE_URL 未设置")
		os.Exit(2)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	// 注意：这个包的**包名是 `skill`（单数）**，虽然目录叫 internal/skills。
	// 第一版我按目录名写成 `skills.NewService`，编译器直接报
	// "imported as skill and not used" / "undefined: skills" —— 记在这里免得再犯。
	store := &skill.Store{Pool: pool}
	svc := skill.NewService(store)

	// 裁判装配：这里没有认证运行时，所以**只认环境变量**（数据库里的中转站
	// 密钥需要 root key 才能解封，批处理不该复制那套逻辑）。
	judge, note, err := skill.OpenABJudge(ctx, nil, nil, skill.ABJudgeOptions{
		BaseURL:     os.Getenv("REPOMESH_AB_BASE_URL"),
		APIKey:      os.Getenv("REPOMESH_AB_API_KEY"),
		Model:       os.Getenv("REPOMESH_AB_MODEL"),
		JudgeModel:  os.Getenv("REPOMESH_AB_JUDGE_MODEL"),
		PreferModel: os.Getenv("REPOMESH_AB_PREFER_MODEL"),
		Label:       os.Getenv("REPOMESH_AB_LABEL"),
	})
	if err != nil {
		fmt.Printf("模型裁判不可用，拒绝写入任何评估记录：%v\n", err)
		os.Exit(3)
	}
	svc.Judge = judge
	fmt.Printf("裁判就绪：%s\n", note)

	rows, err := pool.Query(ctx, `SELECT id::text, coalesce(name,'') FROM public.skills ORDER BY id`)
	if err != nil {
		panic(err)
	}
	type sk struct{ id, name string }
	var all []sk
	for rows.Next() {
		var s sk
		if err := rows.Scan(&s.id, &s.name); err != nil {
			panic(err)
		}
		all = append(all, s)
	}
	rows.Close()
	want := map[string]bool{}
	for _, name := range strings.Split(os.Getenv("REPOMESH_AB_ONLY"), ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			want[trimmed] = true
		}
	}
	fmt.Printf("skills: %d（本次跑 %d 个）\n", len(all), len(want))
	var okN, refusedN, failedN int
	for _, s := range all {
		if len(want) > 0 && !want[s.name] {
			continue
		}
		// 1) 找一个可评版本（evaluating / canary）。
		var versionID string
		err := pool.QueryRow(ctx, `SELECT id::text FROM public.skill_versions
			WHERE skill_id=$1 AND status IN ('evaluating','canary') ORDER BY version LIMIT 1`, s.id).Scan(&versionID)
		if err != nil {
			// 没有 → 从 1.0.0 复制一个 1.1.0:evaluating（正文与 hash 原样，不编内容）。
			err = pool.QueryRow(ctx, `INSERT INTO public.skill_versions
				(skill_id, version, status, content, content_hash, created_by)
				SELECT skill_id, '1.1.0', 'evaluating', content, content_hash, 'ab-batch'
				FROM public.skill_versions WHERE skill_id=$1 AND version='1.0.0'
				RETURNING id::text`, s.id).Scan(&versionID)
			if err != nil {
				fmt.Printf("  x %-30s 建候选版本失败: %v\n", s.name, err)
				failedN++
				continue
			}
		}
		// 2) 跑 A/B —— 与 HTTP 端点同一个服务方法，判定只有一份。
		summary, err := svc.RunABEvaluation(ctx, versionID, "ab-batch")
		if err != nil {
			fmt.Printf("  x %-30s 评估被拒/失败: %v\n", s.name, err)
			refusedN++
			continue
		}
		fmt.Printf("  v %-30s 题数=%d 带技能均分=%.2f 对照均分=%.2f 结论=%s\n",
			s.name, len(summary.Questions), summary.WithMeanScore, summary.WithoutMeanScore, summary.Verdict)
		okN++
	}
	fmt.Printf("\n完成：成功 %d · 被拒 %d · 失败 %d\n", okN, refusedN, failedN)
}
