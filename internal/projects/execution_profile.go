package projects

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/access"
)

// RegisterExecutionProfile 给调用者建一条**执行档案**（含一个可用的 schema2 版本）。
//
// 2026-09-22：这是 A 方案里真正卡住 catmem 的那一环。
//
// 在此之前执行档案**只能由导入路径**（sources/import_v2.go）写入，没有任何 HTTP/UI
// 入口。线上实测 catmem **一个执行档案都没有** —— 于是他的项目走 inherit 解析不到
// 执行侧、建 issue 被「执行配置未完成」永久阻断，而且他自己解不开。
//
// 关键前提（已核实）：**政策是全局的** —— `request_policy_versions` /
// `time_policy_versions` **没有 owner 列**，所以这里直接引用现成的政策版本，
// 不需要先建一套政策子系统。上一轮我把这条依赖估重了，核实后范围收窄。
//
// 政策引用是**必需**的，不是可选：`RegisterCompleteExecution` 会拒收空引用，
// 而一个没有政策钉住的执行档案等于"没有预算与时限约束"—— 那不该被建出来。
func (s *Service) RegisterExecutionProfile(ctx context.Context, principal access.ProjectPrincipal, name string, workerConcurrency int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return "", failure(422, "VALIDATION_FAILED")
	}
	// 上限与 RegisterCompleteExecution 内部校验一致（1..16），这里先挡一道，
	// 让错误在**服务层**以 422 说清，而不是掉进它的 unavailable()。
	if workerConcurrency < 1 || workerConcurrency > 16 {
		return "", failure(422, "VALIDATION_FAILED")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	if err := s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return "", err
	}
	// 取全局政策里**启用/最新**的那一版。取不到就如实拒绝 —— 绝不编一个政策 id 出来，
	// 那会让"没有约束的执行档案"看起来是配好的（正是最难查的一类）。
	var reqID, reqVersion, timeID, timeVersion string
	if err := tx.QueryRow(ctx, `SELECT id::text, version::text
		FROM repomesh_projects.request_policy_versions
		WHERE enabled ORDER BY version DESC LIMIT 1`).Scan(&reqID, &reqVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", failure(409, "POLICY_NOT_CONFIGURED")
		}
		return "", unavailable()
	}
	if err := tx.QueryRow(ctx, `SELECT id::text, version::text
		FROM repomesh_projects.time_policy_versions
		ORDER BY version DESC LIMIT 1`).Scan(&timeID, &timeVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", failure(409, "POLICY_NOT_CONFIGURED")
		}
		return "", unavailable()
	}
	profileID := newCatalogID()
	recipe := ExecutionRecipe{
		WorkerConcurrency:        workerConcurrency,
		VerificationGroupEnabled: false,
		Budget:                   RequestPolicy{Ref: PolicyRef{ID: reqID, Version: reqVersion}},
		Limits:                   TimePolicy{Ref: PolicyRef{ID: timeID, Version: timeVersion}},
	}
	if err := NewCatalogWriter().RegisterCompleteExecution(
		ctx, tx, principal.ActorID(), profileID, "v1", name, recipe); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", unavailable()
	}
	return profileID, nil
}
