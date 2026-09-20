package database

import "testing"

// 内嵌迁移清单必须能加载。加载器会校验"版本号从 1 起连续"——两个文件共用一个
// 版本号（今天线上已两次撞号：hitl_mode 让位、agent_project_scope 让位）在排序后
// 必然断号，在这里就该炸，而不是等到部署时 db migrate 失败、CD 挂在半路。
//
// 这条测试在 0053 重号时确实红过一次（0053_issue_hitl_mode 与
// 0053_repository_team_rooms 并存），随后把后者让到 0056 才转绿。
func TestEmbeddedMigrationsLoad(t *testing.T) {
	if _, err := loadMigrations(migrationFiles); err != nil {
		t.Fatalf("embedded migrations do not load: %v", err)
	}
}
