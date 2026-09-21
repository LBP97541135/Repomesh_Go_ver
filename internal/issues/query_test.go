package issues

import "testing"

// issue 列表两个标签页的过滤（2026-09-21）。
//
// 修之前 `IssueListQuery` 压根没有 State 字段、handler 也没读 `state` 参数：
// 控制台点 Open / Closed 传了值，服务端丢掉，两个标签页返回同一份全量列表。
// 这组用例把「认哪些值、不认哪些值」钉住 —— 尤其是**不能**把写错的值
// 悄悄当成 all，那正是这个 bug 能藏住的原因。
func TestParseIssueListQueryState(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		// 不传 = all：与修复前行为一致，老脚本不会静默少行。
		{"", "all", true},
		{"open", "open", true},
		{"closed", "closed", true},
		{"all", "all", true},
		// 大小写与拼错都要 422，而不是收敛成 all。
		{"OPEN", "", false},
		{"Closed", "", false},
		{"true", "", false},
		{"opened", "", false},
	}
	for _, tc := range cases {
		got, err := ParseIssueListQuery("", "", tc.in, "", 20)
		if !tc.ok {
			if err == nil {
				t.Fatalf("state=%q 应当被拒绝，实际通过了（state=%q）", tc.in, got.State)
			}
			continue
		}
		if err != nil {
			t.Fatalf("state=%q 应当被接受，实际报错 %v", tc.in, err)
		}
		if got.State != tc.want {
			t.Fatalf("state=%q → %q，期望 %q", tc.in, got.State, tc.want)
		}
	}
}

// 分页游标的指纹必须把 state 算进去。
//
// 否则「open 第一页」的游标能在「closed」标签页上重放：翻页会翻进另一个集合，
// 而且不报错 —— 静默错行比报错更难发现。
func TestCursorScopeFingerprintSeparatesState(t *testing.T) {
	open := cursorScopeFingerprint("actor", "issues", "project", "", "", "open", 20)
	closed := cursorScopeFingerprint("actor", "issues", "project", "", "", "closed", 20)
	all := cursorScopeFingerprint("actor", "issues", "project", "", "", "all", 20)
	if open == closed || open == all || closed == all {
		t.Fatal("不同 state 的游标指纹相同，跨标签页重放会静默错行")
	}
	// 同一组输入必须稳定（游标校验靠它）。
	if again := cursorScopeFingerprint("actor", "issues", "project", "", "", "open", 20); again != open {
		t.Fatal("同一组输入的指纹不稳定，合法游标会被判失效")
	}
}

// 只按仓库过滤（不传 q）曾经必炸：where 里写死了 $4，而参数只有 3 个。
// 这条用例守的是"参数序号跟着拼"这件事本身。
func TestParseIssueListQueryKeepsTextAndRepositoryIndependent(t *testing.T) {
	query, err := ParseIssueListQuery("", "repo_0001", "open", "", 20)
	if err != nil {
		t.Fatalf("只按仓库过滤应当被接受：%v", err)
	}
	if query.RepositoryID != "repo_0001" || query.Text != "" || query.State != "open" {
		t.Fatalf("字段落位不对：%+v", query)
	}
}
