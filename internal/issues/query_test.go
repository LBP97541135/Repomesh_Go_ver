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
		got, err := ParseIssueListQuery("", "", tc.in, false, "", 20)
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
	fp := func(state string, includeArchived bool) string {
		return cursorScopeFingerprint("actor", "issues", "project",
			IssueListQuery{State: state, IncludeArchived: includeArchived, Limit: 20})
	}
	open := fp("open", false)
	closed := fp("closed", false)
	all := fp("all", false)
	if open == closed || open == all || closed == all {
		t.Fatal("不同 state 的游标指纹相同，跨标签页重放会静默错行")
	}
	// include_archived 也必须进指纹：开关一切换，结果集就换了（墓碑行加进来），
	// 旧游标接着翻会翻进另一个集合 —— 同样是不报错的静默错行。
	if open == fp("open", true) {
		t.Fatal("include_archived 没进游标指纹，切开关后旧游标会静默错行")
	}
	// 同一组输入必须稳定（游标校验靠它）。
	if again := fp("open", false); again != open {
		t.Fatal("同一组输入的指纹不稳定，合法游标会被判失效")
	}
}

// 只按仓库过滤（不传 q）曾经必炸：where 里写死了 $4，而参数只有 3 个。
// 这条用例守的是"参数序号跟着拼"这件事本身。
func TestParseIssueListQueryKeepsTextAndRepositoryIndependent(t *testing.T) {
	query, err := ParseIssueListQuery("", "repo_0001", "open", false, "", 20)
	if err != nil {
		t.Fatalf("只按仓库过滤应当被接受：%v", err)
	}
	if query.RepositoryID != "repo_0001" || query.Text != "" || query.State != "open" {
		t.Fatalf("字段落位不对：%+v", query)
	}
}

// include_archived 是列表右上角「已归档」开关的语义：它此前被后端忽略，
// 是个按下去没有任何变化的哑开关。这里钉住它真的被解析进了查询。
func TestParseIssueListQueryIncludeArchived(t *testing.T) {
	off, err := ParseIssueListQuery("", "", "all", false, "", 20)
	if err != nil {
		t.Fatalf("不带 include_archived 应当被接受：%v", err)
	}
	if off.IncludeArchived {
		t.Fatal("缺省必须是不带墓碑行（= 修复前的行为），不能默认打开")
	}
	on, err := ParseIssueListQuery("", "", "all", true, "", 20)
	if err != nil {
		t.Fatalf("带 include_archived 应当被接受：%v", err)
	}
	if !on.IncludeArchived {
		t.Fatal("include_archived=true 没有落进查询，开关会是个哑开关")
	}
}
