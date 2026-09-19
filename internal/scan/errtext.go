package scan

import (
	"errors"
	"fmt"
	"strings"

	"repomesh.local/repomesh/internal/reposcan"
)

// HumanScanError 把 reposcan 的错误类别翻成**用户能据以自救**的一句话。
//
// 为什么不能直接把 err.Error() 交给界面：那串东西是给日志看的
// （"reposcan: not found" / "reposcan: platform unavailable: HTTP 404"），
// 用户看到它既不知道是自己贴错了链接、还是私有仓没权限、还是平台抽风 ——
// 而这三件事的下一步动作完全不同（改链接 / 换账号登录 / 等一会儿重扫）。
//
// 2026-09-20 事故：把个人主页链接当仓库链接贴（…/LBP97541135/LBP97541135）→
// GitHub 404 → 界面原样显示 "reposcan: not found"，用户只能来问人。
//
// 措辞一律**只讲事实 + 下一步**，不回显出站细节。
func HumanScanError(err error, kind, url, tokenSource string) string {
	switch {
	case errors.Is(err, reposcan.ErrNotFound):
		base := "GitHub 回答 404：这个地址不存在，或者是私有仓库而当前凭据看不到它。" +
			"请确认链接拼写——组织链接只有一段路径（github.com/组织名），仓库链接有两段（github.com/组织名/仓库名）。" +
			"私有仓请用有权限的账号登录后重扫。"
		if hint := profilePastedAsRepoHint(kind, url); hint != "" {
			base += hint
		}
		return base
	case errors.Is(err, reposcan.ErrRateLimited):
		return fmt.Sprintf("GitHub 限流：本轮已中止，没试过的仓库不计入失败（本次凭据：%s）。稍后重扫即可。",
			tokenLabel(tokenSource))
	case errors.Is(err, reposcan.ErrUnauthorized):
		return fmt.Sprintf("GitHub 拒绝了这个凭据（本次凭据：%s）：重新登录，或确认该账号能读这些仓库。",
			tokenLabel(tokenSource))
	case errors.Is(err, reposcan.ErrUnavailable):
		return "GitHub 暂时不可用（" + err.Error() + "）：稍后重扫。"
	default:
		return err.Error()
	}
}

// profilePastedAsRepoHint 识别「把主页链接当成仓库链接」这个具体误用。
//
// 判据是**路径两段同名**（…/LBP97541135/LBP97541135）：GitHub 上不存在
// owner 与 repo 同名且真的属于该 owner 的仓库，所以这条 404 几乎总是贴错。
// 认不出来就返回空串 —— 不猜，也不把普通 404 硬说成贴错。
func profilePastedAsRepoHint(kind, url string) string {
	if kind != "repository" {
		return ""
	}
	segments, ok := reposcan.SplitRepoPath(strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git"))
	if !ok || len(segments) != 2 {
		return ""
	}
	if !strings.EqualFold(segments[0], segments[1]) {
		return ""
	}
	return fmt.Sprintf("这条链接看起来是主页而不是仓库（两段路径同名：%s/%s）——要批量扫这个账号请只填一段路径。",
		segments[0], segments[1])
}

// tokenLabel 如实说明这次扫描用的是谁的凭据（与 ScanJob.TokenSource 同源）。
func tokenLabel(tokenSource string) string {
	switch tokenSource {
	case "user":
		return "你自己的 GitHub 令牌"
	case "deployment":
		return "部署级只读令牌"
	case "anonymous":
		return "匿名（未登录，配额最低）"
	default:
		return "未知"
	}
}
