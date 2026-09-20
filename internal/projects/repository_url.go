package projects

import (
	"errors"
	"net/url"
	"strings"
)

// ErrUnsupportedRepositoryURL 表示这串东西不是"一个 GitHub 仓库的地址"。
var ErrUnsupportedRepositoryURL = errors.New("projects: unsupported repository url")

/** ParseRepositoryURL 从仓库 URL 里取出 host / owner / name。
 *
 *  2026-09-20 加：接入接口要吃"人看得见的东西"。此前只吃 `repo_<20 位数字>`，
 *  而那个 id 只有发现面给得出来，仓库页（扫描目录）给不出——前端于是必然 422。
 *  URL 两边都有，所以把它作为入口。
 *
 *  只认 `https://github.com/{owner}/{name}` 这一类形态（扫描目录与项目读面给的都是
 *  这个），宽松掉尾部 `.git` 与结尾斜杠——粘贴和导出的来源都会带这两种尾巴。
 *  这里**不访问网络**："这个仓是否存在、你有没有权"由参与权观测回答。 */
func ParseRepositoryURL(raw string) (host, owner, name string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", "", ErrUnsupportedRepositoryURL
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 2 || segments[0] == "" {
		return "", "", "", ErrUnsupportedRepositoryURL
	}
	name = strings.TrimSuffix(segments[1], ".git")
	if strings.TrimSpace(name) == "" {
		return "", "", "", ErrUnsupportedRepositoryURL
	}
	return "github.com", segments[0], name, nil
}
