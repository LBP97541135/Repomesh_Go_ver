package projects

import "testing"

// 2026-09-20：这一组守着「接入按 URL」的入口。
//
// 起因是线上实测：用户点「接入本项目」四次，后端四次 422 —— 前端拿的是**扫描目录**
// 的 id（32 位随机 hex），而更新接口只吃 `repo_<20 位数字>`。现在入口改成 URL，
// 所以"哪些 URL 算数"就成了新的一道门，必须有测试钉住：
// 太严会把真实数据挡在外面（扫描目录给的正是这些形状），
// 太松会把垃圾放进网络请求。
func TestParseRepositoryURL(t *testing.T) {
	good := []struct {
		in                string
		host, owner, name string
	}{
		{"https://github.com/LBP97541135/repomesh-e2e-api", "github.com", "LBP97541135", "repomesh-e2e-api"},
		{"https://github.com/LBP97541135/repomesh-e2e-api.git", "github.com", "LBP97541135", "repomesh-e2e-api"},
		{"https://github.com/repomesh-train-ticket/ts-voucher-service/", "github.com", "repomesh-train-ticket", "ts-voucher-service"},
		{"  https://github.com/o/n  ", "github.com", "o", "n"},
	}
	for _, c := range good {
		host, owner, name, err := ParseRepositoryURL(c.in)
		if err != nil || host != c.host || owner != c.owner || name != c.name {
			t.Fatalf("ParseRepositoryURL(%q) = (%q,%q,%q,%v)，期望 (%q,%q,%q,nil)", c.in, host, owner, name, err, c.host, c.owner, c.name)
		}
	}

	// 这些必须拒：不是 https、不是 github.com、指到组织而不是仓、带查询串或片段。
	bad := []string{
		"",
		"git@github.com:o/n.git",
		"http://github.com/o/n",
		"https://gitlab.com/o/n",
		"https://github.com/o",
		"https://github.com/o/n/tree/main",
		"https://github.com/o/n?tab=readme",
		"https://github.com/o/n#readme",
		"https://user@github.com/o/n",
	}
	for _, c := range bad {
		if _, _, _, err := ParseRepositoryURL(c); err == nil {
			t.Fatalf("ParseRepositoryURL(%q) 应当报错，却通过了", c)
		}
	}
}

// parseRepositoryURLs 是请求体那一层的门：空数组、超 100、非字符串、重复项。
func TestParseRepositoryURLsRejectsBadShapes(t *testing.T) {
	if _, err := parseRepositoryURLs([]byte(`[]`)); err == nil {
		t.Fatal("空数组应当拒")
	}
	if _, err := parseRepositoryURLs([]byte(`["https://gitlab.com/o/n"]`)); err == nil {
		t.Fatal("非 github.com 应当拒")
	}
	if _, err := parseRepositoryURLs([]byte(`[123]`)); err == nil {
		t.Fatal("非字符串应当拒")
	}
	// 重复项去重后只留一条（同一串 URL 传两次不该变成两次网络请求）
	value, err := parseRepositoryURLs([]byte(`["https://github.com/o/n","https://github.com/o/n"]`))
	if err != nil || len(value) != 1 {
		t.Fatalf("重复项应去重为 1 条，得到 %v err=%v", value, err)
	}
}
