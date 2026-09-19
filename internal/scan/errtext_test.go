package scan

import (
	"fmt"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/reposcan"
)

// 界面上的「扫描失败：…」直接吃这个函数的输出，所以它必须做到两件事：
//
//	① 不把内部错误串（"reposcan: not found"）原样交给用户；
//	② 每一条都说清「下一步做什么」。
//
// 2026-09-20 事故：把个人主页链接当仓库链接贴（…/LBP97541135/LBP97541135）→
// GitHub 404 → 界面原样显示 "reposcan: not found"，用户只能来问人。
func TestHumanScanErrorIsActionable(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		kind        string
		url         string
		tokenSource string
		wantAll     []string
		wantNone    []string
	}{
		{
			name: "404 给链接拼写与私有仓两条出路",
			err:  reposcan.ErrNotFound, kind: "organization", url: "https://github.com/nobody",
			wantAll:  []string{"404", "确认链接拼写", "私有仓", "登录后重扫"},
			wantNone: []string{"reposcan:"},
		},
		{
			name: "主页当仓库贴时点出具体误用",
			err:  reposcan.ErrNotFound, kind: "repository", url: "https://github.com/LBP97541135/LBP97541135",
			wantAll: []string{"主页", "LBP97541135/LBP97541135", "只填一段路径"},
		},
		{
			name: "同名两段只对仓库扫描生效",
			err:  reposcan.ErrNotFound, kind: "organization", url: "https://github.com/acme/acme",
			wantNone: []string{"主页"},
		},
		{
			name: "不同名的普通 404 不硬说是贴错",
			err:  reposcan.ErrNotFound, kind: "repository", url: "https://github.com/acme/ghost",
			wantNone: []string{"主页"},
		},
		{
			name: "限流说清用的是谁的凭据、且不计失败",
			err:  reposcan.ErrRateLimited, kind: "organization", url: "https://github.com/acme", tokenSource: "anonymous",
			wantAll:  []string{"限流", "不计入失败", "匿名"},
			wantNone: []string{"reposcan:"},
		},
		{
			name: "凭据被拒指向重新登录",
			err:  reposcan.ErrUnauthorized, kind: "organization", url: "https://github.com/acme", tokenSource: "user",
			wantAll: []string{"拒绝了这个凭据", "你自己的 GitHub 令牌", "重新登录"},
		},
		{
			name: "平台不可用保留出站细节供排查",
			err:  fmt.Errorf("%w: HTTP 502", reposcan.ErrUnavailable), kind: "organization", url: "https://github.com/acme",
			wantAll: []string{"暂时不可用", "HTTP 502", "稍后重扫"},
		},
		{
			name: "认不出的错误原样上抛，不编",
			err:  fmt.Errorf("scan: write failed"), kind: "organization", url: "https://github.com/acme",
			wantAll: []string{"scan: write failed"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := HumanScanError(test.err, test.kind, test.url, test.tokenSource)
			if got == "" {
				t.Fatal("不该给出空文案")
			}
			for _, want := range test.wantAll {
				if !strings.Contains(got, want) {
					t.Fatalf("文案应含 %q，得到 %q", want, got)
				}
			}
			for _, none := range test.wantNone {
				if strings.Contains(got, none) {
					t.Fatalf("文案不该含 %q，得到 %q", none, got)
				}
			}
		})
	}
}

// 文案是给界面直接渲染的纯文本：markdown 星号会原样显示出来。
func TestHumanScanErrorHasNoMarkdown(t *testing.T) {
	for _, err := range []error{reposcan.ErrNotFound, reposcan.ErrRateLimited, reposcan.ErrUnauthorized, reposcan.ErrUnavailable} {
		got := HumanScanError(err, "repository", "https://github.com/acme/acme", "anonymous")
		if strings.Contains(got, "**") {
			t.Fatalf("界面是纯文本渲染，不该带 markdown：%q", got)
		}
	}
}
