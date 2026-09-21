package issues

import (
	"strings"
	"testing"
)

// 2026-09-21 线上：工作台建项**全量 422 VALIDATION_FAILED**，页面只说得出
// "The request could not be completed."，日志里也只有 `err=issues: VALIDATION_FAILED`
// —— 连字段名都没有。
//
// 根因不是哪条取值非法，而是 known 白名单没跟着新字段长：`executionMode` /
// `requiredCheckpoints` 明明在 parseNewInput 下面被读（三档监管强度改造加的），
// 却不算"认识的键"，于是带这两个键的请求一律按**未知字段**拒掉。而三档改造之后
// 前端每次建项都会带上它们（executionMode 恒有值、requiredCheckpoints 恒为数组，
// 空数组在 JS 里也是真值所以要发），所以断的不是某一条分支，是整条建项路。
//
// 这条用例钉的就是**前端实际发的那份载荷**（键集合取自 WorkbenchPage 的
// attempt.current.input + api/issues.ts 的 createIssue 组装），三档各发一次。
// 谁再往请求里加键而忘了让白名单跟着长，这里先红。
func TestWorkbenchCreationPayloadIsAccepted(t *testing.T) {
	base := `"expectedCreationContextRevision":"ctx-1","conversation":{"mode":"new"},` +
		`"title":"TT-001：订票与取消流程通知邮件修复需求","description":"需求正文"`

	cases := []struct {
		name            string
		body            string
		hitlMode        string
		executionMode   string
		wantCheckpoints int
	}{
		{
			// 全自动：前端 executionTier=unattended → hitlMode ai / auto / 空卡点
			name:          "全自动（unattended）",
			body:          `{` + base + `,"hitlMode":"ai","mergeMode":"manual","executionMode":"auto","requiredCheckpoints":[]}`,
			hitlMode:      "ai",
			executionMode: "auto",
		},
		{
			// 半自动：自选卡点，前端按 CHECKPOINT_ORDER 排序后发
			name:            "半自动（key_points，自选两个卡点）",
			body:            `{` + base + `,"hitlMode":"ai","mergeMode":"manual","executionMode":"supervised","requiredCheckpoints":["delivery","repository_scope"]}`,
			hitlMode:        "ai",
			executionMode:   "supervised",
			wantCheckpoints: 2,
		},
		{
			// 人工审核：前端不发卡点（省一次往返），由后端补满六个
			name:            "人工审核（every_step，卡点由后端补齐）",
			body:            `{` + base + `,"hitlMode":"hitl","mergeMode":"manual","executionMode":"manual_controlled"}`,
			hitlMode:        "hitl",
			executionMode:   "manual_controlled",
			wantCheckpoints: 6,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input, err := parseNewInput([]byte(c.body))
			if err != nil {
				t.Fatalf("工作台发的载荷必须收下，却拒了：%v", err)
			}
			if input.executionMode != c.executionMode {
				t.Fatalf("executionMode = %q, want %q", input.executionMode, c.executionMode)
			}
			if input.hitlMode != c.hitlMode {
				t.Fatalf("hitlMode = %q, want %q", input.hitlMode, c.hitlMode)
			}
			if len(input.requiredCheckpoints) != c.wantCheckpoints {
				t.Fatalf("卡点数 = %d, want %d（%v）",
					len(input.requiredCheckpoints), c.wantCheckpoints, input.requiredCheckpoints)
			}
		})
	}
}

// 上一条用例钉"该收的收下"，这条钉"该拒的仍拒"——白名单补键不能顺手把域不变量
// 也放宽了（auto 不许带卡点、半自动至少要一个卡点，见迁移 0065 的 CHECK）。
func TestExecutionTierInvariantsStillHold(t *testing.T) {
	base := `"expectedCreationContextRevision":"ctx-1","conversation":{"mode":"new"},"title":"t","description":"d"`
	cases := []struct {
		name       string
		body       string
		wantErrHas string
	}{
		{
			name:       "全自动带卡点必须拒",
			body:       `{` + base + `,"executionMode":"auto","requiredCheckpoints":["delivery"]}`,
			wantErrHas: "requiredCheckpoints",
		},
		{
			name:       "半自动一个卡点都不勾必须拒",
			body:       `{` + base + `,"executionMode":"supervised","requiredCheckpoints":[]}`,
			wantErrHas: "requiredCheckpoints",
		},
		{
			name:       "三档以外的取值必须拒",
			body:       `{` + base + `,"executionMode":"sometimes"}`,
			wantErrHas: "executionMode",
		},
		{
			name:       "卡点名不在六个里必须拒",
			body:       `{` + base + `,"executionMode":"supervised","requiredCheckpoints":["made_up_checkpoint"]}`,
			wantErrHas: "requiredCheckpoints",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseNewInput([]byte(c.body))
			if err == nil {
				t.Fatal("应当被拒，却收下了")
			}
			if !strings.Contains(err.Error(), c.wantErrHas) {
				t.Fatalf("报错必须点名 %q，got %v", c.wantErrHas, err)
			}
		})
	}
}

// 真未知的键仍然要拒，但**必须点名**——上一版这里不带字段名，线上只能靠日志里
// 那行 body 头猜是哪个键；白名单漏一个键就会让整条建项路静默 422。
func TestUnknownCreationKeyIsRejectedByName(t *testing.T) {
	body := `{"expectedCreationContextRevision":"ctx-1","conversation":{"mode":"new"},` +
		`"title":"t","description":"d","executionTier":"unattended"}`
	_, err := parseNewInput([]byte(body))
	if err == nil {
		t.Fatal("未知键应当被拒，却收下了")
	}
	if !strings.Contains(err.Error(), "executionTier") {
		t.Fatalf("报错必须点名未知键 executionTier，got %v", err)
	}
}
