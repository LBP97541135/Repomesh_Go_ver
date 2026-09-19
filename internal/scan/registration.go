package scan

import "context"

// RegistrationCounts is what the scan UI reports after applying scan output
// to the catalog. Total counts every scanned repository; Registered includes
// refreshes because a catalog write happened; Failed means "did not make it
// into the catalog", whatever the reason.
type RegistrationCounts struct {
	Total      int
	Registered int
	Skipped    int
	Failed     int
	// RateLimited 是"因平台限流而**根本没尝试**"的仓库数，AbortedReason 非空
	// 表示本轮被限流提前中止。
	//
	// 2026-09-19 事故：配额是共享的，撞上限流后继续把剩余仓库一个个打过去只会
	// 全部失败，还白白烧掉配额恢复前的机会。所以命中限流即中止本轮，并把没试过的
	// 如实计在这里——它们既不是 failed（试过了没成功），也不是普通的 skipped。
	RateLimited   int
	AbortedReason string
}

// RegisterScanned applies scan output to the catalog:
//
//   - a repository whose scan failed is never registered as a plausible empty
//     card (counted as failed);
//   - an existing name with a fresh card is refreshed — whole-card replace,
//     operator-owned fields untouched (re-scanning is the only retry the UI
//     offers, so it must be safe to repeat and must not go stale);
//   - an existing name with no card is skipped;
//   - a new name is registered.
//
// One repository's write failure is counted, not raised: thirty-nine good
// repositories must survive the fortieth failing one.
// organizationScopedStore 是可选能力：PostgresCatalog 实现它（写入时盖空间章）。
// 测试用的内存目录不实现 —— 那时退回全局写（测试里只有一个空间，没有隔离诉求）。
type organizationScopedStore interface {
	AddInOrganization(ctx context.Context, organizationID string, card RepositoryCard) error
	StampOrganization(ctx context.Context, id, organizationID string) error
}

// RegisterScanned 是**全局**写入路径（不盖空间章）。保留它只为兼容既有测试；
// 生产路径一律走 RegisterScannedInOrganization。
func RegisterScanned(ctx context.Context, store CatalogStore, profiles []RepositoryCard) (RegistrationCounts, error) {
	return registerScanned(ctx, store, profiles, "")
}

// RegisterScannedInOrganization 是空间感知的写入路径：新卡片盖上 organizationID，
// 刷新既有行时补章（迁移前登记的老行没有归属）。
//
// 2026-09-19 账号隔离：扫描目录（repomesh_scan.repositories）的 organization_id
// 列一直存在却从没写过，读面因此全库可见。写入盖章 + 读面裁剪才闭合。
func RegisterScannedInOrganization(ctx context.Context, store CatalogStore, organizationID string, profiles []RepositoryCard) (RegistrationCounts, error) {
	return registerScanned(ctx, store, profiles, organizationID)
}

func registerScanned(ctx context.Context, store CatalogStore, profiles []RepositoryCard, organizationID string) (RegistrationCounts, error) {
	counts := RegistrationCounts{Total: len(profiles)}
	scoped, _ := store.(organizationScopedStore)

	rows, err := store.List(ctx)
	if err != nil {
		return counts, err
	}
	existing := make(map[string]RepositoryCard, len(rows))
	for _, row := range rows {
		existing[row.Name] = row
	}

	for _, profile := range profiles {
		if profile.ScanStatus == ScanStatusFailed {
			counts.Failed++
			continue
		}
		seen, exists := existing[profile.Name]
		if !exists {
			var addErr error
			if scoped != nil {
				addErr = scoped.AddInOrganization(ctx, organizationID, profile)
			} else {
				addErr = store.Add(ctx, profile)
			}
			if addErr != nil {
				counts.Failed++
				continue
			}
			existing[profile.Name] = profile
			counts.Registered++
			continue
		}
		if profile.AutoCard == nil {
			counts.Skipped++
			continue
		}
		if err := store.UpdateAutoCard(ctx, seen.ID, *profile.AutoCard, profile.Languages, profile.Fingerprint); err != nil {
			counts.Failed++
			continue
		}
		if scoped != nil {
			// 刷新既有行时补章：迁移前登记的老行没有空间归属。
			_ = scoped.StampOrganization(ctx, seen.ID, organizationID)
		}
		updated, err := store.Get(ctx, seen.ID)
		if err != nil || updated == nil {
			counts.Failed++
			continue
		}
		existing[profile.Name] = *updated
		counts.Registered++
	}
	return counts, nil
}
