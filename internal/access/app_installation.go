package access

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"repomesh.local/repomesh/internal/github"
)

// AppInstallTarget 是「一个需要（或已经）装了 App 的账号」——界面上就是一行。
//
// 粒度是**账号**，不是仓库：一个组织装一次就覆盖它名下所有仓（选了
// All repositories 的话，连以后新建的也覆盖）。所以这个读面的用途是
// 「还差哪几个账号」，而不是「还差哪些仓」。
type AppInstallTarget struct {
	Login string `json:"login"`
	/** user | organization —— 决定"这个人能不能自己装"。 */
	Kind      string `json:"kind"`
	Installed bool   `json:"installed"`
	/** all | selected。
	 *  `selected` **不代表"缺了仓"**——只说明安装时是按仓库挑的，挑中的完全可能正好
	 *  就是我要用的那些。账号级看不到"某个仓在不在里面"，所以界面对它只能说
	 *  「按仓库挑选的安装，哪个仓不可选就去设置页把它加进来」，**不能断言"还差 N 个仓"**。 */
	RepositorySelection string `json:"repositorySelection,omitempty"`
	Suspended           bool   `json:"suspended,omitempty"`
	/** 没装时 = 安装链接（带 suggested_target_id，直接落在那个账号上）；
	 *  已装时 = 那个 installation 的设置页（补勾仓库用）。 */
	InstallURL string `json:"installUrl"`
	/** 当前用户有没有权限在这个账号上装。
	 *  **目前恒为 null**：判断组织角色要 `read:org` scope，而本部署的登录流程刻意
	 *  不申请任何 scope；填一个必然失败的调用等于埋死代码。所以这件事交给**安装页
	 *  自己**回答——GitHub 的 installations/new 只列出你能装的账号。
	 *  字段保留在契约里，将来决定加 scope 时不用再改形状。 */
	CanInstall   *bool    `json:"canInstall"`
	Repositories []string `json:"repositories"`
}

type AppInstallationView struct {
	Targets []AppInstallTarget `json:"targets"`
	/** 仓所在账号**完全没装** App（或被挂起）的仓数——这两种情况下那些仓一个都用不了，
	 *  是确定的。0 = 账号级没有缺口，这件事不该再出现在界面上。
	 *  **`selected` 安装不计入**：账号级看不到逐仓归属，算它就是假报（见
	 *  `RepositorySelection` 的说明）。 */
	UncoveredCount int `json:"uncoveredCount"`
	/** App 侧探测失败时的**如实说明**——既不假装已装好，也不假装没装。 */
	Unavailable string `json:"unavailable,omitempty"`
}

// AppInstallationStatus 回答三个问题：我的仓都归哪些账号所有？哪些还没装 App？
// 我自己能不能装那几个？
//
// 只读：**不创建安装**。GitHub 没有"安装 App"的 API（只有列出/读/铸令牌/删），
// 安装是刻意的同意步骤，必须由有权限的人在网页上点。这里能做的是把"要点几次、
// 点哪个链接、你有没有权限"算准，把人工压到最少。
func (s *Service) AppInstallationStatus(ctx context.Context, principal ProjectPrincipal) (AppInstallationView, error) {
	// 这个人的项目里有哪些仓（按 owner 账号归拢）。**只列不属于任何项目的仓没有
	// 意义**——建 issue 的授权范围是项目里的仓，目录里那些无关的账号不该被催着装。
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT r.owner, r.name
		FROM repomesh_projects.project_repositories pr
		JOIN repomesh_projects.projects p ON p.id = pr.project_id
		JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
		WHERE p.owner = $1 AND p.removed_at IS NULL
		ORDER BY 1, 2`, principal.actor)
	if err != nil {
		return AppInstallationView{}, unavailable()
	}
	defer rows.Close()
	byOwner := map[string][]string{}
	for rows.Next() {
		var owner, name string
		if err := rows.Scan(&owner, &name); err != nil {
			return AppInstallationView{}, unavailable()
		}
		byOwner[owner] = append(byOwner[owner], owner+"/"+name)
	}
	if err := rows.Err(); err != nil {
		return AppInstallationView{}, unavailable()
	}

	view := AppInstallationView{Targets: []AppInstallTarget{}}
	if len(byOwner) == 0 {
		return view, nil
	}

	// 用类型断言拿底层客户端（沿 deployment.go 的惯例：不往 Provider 接口上加方法，
	// 否则测试里的假 provider 全都要补实现）。拿不到就如实说"这个部署没有 App 凭据"。
	client := s.GitHubAppClient()
	if client == nil {
		view.Unavailable = "这个部署没有配置 GitHub App 凭据，读不到安装状态。"
		return view, nil
	}
	slug, err := client.AppSlug(ctx)
	if err != nil {
		view.Unavailable = "取 GitHub App 信息失败：" + err.Error()
		return view, nil
	}
	installations, err := client.AppInstallations(ctx)
	if err != nil {
		view.Unavailable = "取 App 安装列表失败：" + err.Error()
		return view, nil
	}
	installedBy := map[string]github.AppInstallationSummary{}
	for _, installed := range installations {
		installedBy[strings.ToLower(installed.AccountLogin)] = installed
	}

	// 用户令牌：`/users/{login}` 不能用 App JWT 调（线上探针实测被判 unauthorized），
	// 所以带上用户的令牌；取不到就匿名调——那是公开数据，够这次引导用。
	var userToken string
	if credential, err := s.credential(ctx, principal.actor, false); err == nil {
		userToken = credential.token
	}

	logins := make([]string, 0, len(byOwner))
	for login := range byOwner {
		logins = append(logins, login)
	}
	sort.Strings(logins)

	accountURL := func(login, kind string, installationID int64) string {
		if kind == "organization" {
			return fmt.Sprintf("https://github.com/organizations/%s/settings/installations/%d", url.PathEscape(login), installationID)
		}
		return fmt.Sprintf("https://github.com/settings/installations/%d", installationID)
	}
	installURL := func(id int64) string {
		return fmt.Sprintf("https://github.com/apps/%s/installations/new?suggested_target_id=%d", url.PathEscape(slug), id)
	}

	for _, login := range logins {
		repositories := byOwner[login]
		sort.Strings(repositories)
		target := AppInstallTarget{Login: login, Kind: "user", Repositories: repositories}

		if installed, ok := installedBy[strings.ToLower(login)]; ok {
			if strings.EqualFold(installed.AccountType, "Organization") {
				target.Kind = "organization"
			}
			target.Installed = true
			target.RepositorySelection = installed.RepositorySelection
			target.Suspended = installed.Suspended
			target.InstallURL = accountURL(login, target.Kind, installed.ID)
			// **只有**"被挂起"才计入未覆盖：那种情况下这个账号名下的仓一个都用不了，
			// 是确定的。
			//
			// `repositorySelection == "selected"` **不计**——这一条我一开始写错了，
			// 线上数据把它抓了出来：你的两个仓都归一个"装了但按仓库挑选"的账号，
			// 硬算成"没覆盖"会得出 uncoveredCount=2，而那两个仓其实是好的（能在上面
			// 建 issue）。账号级**看不到**"某个仓在不在选中列表里"，算它就是假报，
			// 还会催人去补勾本来就好的仓。
			// 逐仓的真相由**逐仓的 App 观测**回答（仓库页那列「App 工作授权就绪/不足」
			// 就是它，`allProjectRepositories` 读的同一份结论），这里不重复猜。
			if installed.Suspended {
				view.UncoveredCount += len(repositories)
			}
			view.Targets = append(view.Targets, target)
			continue
		}

		// 没装：拼安装链接。账号信息拿不到时退化成不带预选账号的通用链接——
		// 只是让用户多挑一下账号，不影响能不能装。
		id, kind, profileErr := client.AccountProfile(ctx, userToken, login)
		if profileErr != nil {
			target.InstallURL = "https://github.com/apps/" + url.PathEscape(slug) + "/installations/new"
		} else {
			if kind == "Organization" {
				target.Kind = "organization"
			}
			target.InstallURL = installURL(id)
		}
		view.UncoveredCount += len(repositories)
		view.Targets = append(view.Targets, target)
	}
	return view, nil
}
