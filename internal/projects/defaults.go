package projects

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/access"
)

// SetDefaultProfile 把某个档案设成调用者在该 kind 上的**默认**。
//
// 2026-09-21：这是**补上的能力**，不是重构。
//
// 在此之前 `repomesh_projects.defaults` 表**只有导入路径**（sources/import_v2.go）
// 能写，没有任何 HTTP 入口；而 `model` 那一侧**连写函数都不存在**
// （只有 BindExecutionDefault，kind 硬编码 'execution'）。后果在线上实测到了：
// catmem 明明有一个完整可用的模型档案（deepseek，enabled，parameters_complete），
// 却**没有任何办法把它设成默认** —— 于是他的项目走 `inherit` 解析到空，
// 建 issue 被「执行配置未完成」**永久**阻断，而且他自己解不开。
//
// 判据刻意写死在这里，不散到调用方：
//   - kind 必须是闭集里的一个（写错不是"多一条脏数据"，是让 inherit 永远解析不到）；
//   - 档案必须**属于调用者**、kind 对得上、**enabled**、且**当前版本行存在** ——
//     否则设出来的"默认"是个空壳，下一次建单照样解析不出来，
//     而用户会以为"我已经设过了"（那正是最难查的一类故障）。
//
// 版本号**不接受调用方传值**：默认永远指向档案的 current_version，
// 否则会出现"默认指向一个已经不是当前的版本"，与档案页显示的不一致。
func (s *Service) SetDefaultProfile(ctx context.Context, principal access.ProjectPrincipal, kind, profileID string) error {
	if kind != "model" && kind != "execution" {
		return failure(422, "VALIDATION_FAILED")
	}
	if profileID == "" {
		return failure(422, "VALIDATION_FAILED")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	var currentVersion string
	err = tx.QueryRow(ctx, `
		SELECT p.current_version
		FROM repomesh_projects.profiles p
		JOIN repomesh_projects.profile_versions v
		  ON v.kind = p.kind AND v.profile_id = p.id AND v.version = p.current_version
		WHERE p.owner = $1 AND p.kind = $2 AND p.id = $3 AND p.enabled`,
		principal.ActorID(), kind, profileID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		// 不存在 / 不是你的 / 没启用 / 当前版本行缺失 —— 一律 404，
		// 不区分"不存在"与"不是你的"（与项目归档那条同一取向，不泄露存在性）。
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	if _, err := NewCatalogWriter().BindDefault(ctx, tx, principal.ActorID(), kind, profileID, currentVersion); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}
