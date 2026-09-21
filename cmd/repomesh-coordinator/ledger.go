package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
	skill "repomesh.local/repomesh/internal/skills"
	"repomesh.local/repomesh/internal/typesafe"
)

// coordinatorLedger adapts the coordinator to the tasks.ExecutionFacade: it
// reserves real worker attempts in the B10 ledger so the DAG dispatch path
// runs identically to the host-executor path (same rows, same invariants).
type coordinatorLedger struct {
	pool *pgxpool.Pool
}

// newRunID returns a random run/attempt id; the old timestamp-derived ids
// collided under concurrent dispatches and the ON CONFLICT DO NOTHING
// swallowed the insert, leaving the caller with a dangling attempt id.
func newRunID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("coordinator: id generation failed: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}

// sanitizeSingleQuoted makes text safe inside the single-quoted argv token
// restored by the executor's quote-aware splitter (and inside double-quoted
// git/curl arguments): quotes, NUL and shell-active characters are dropped.
func sanitizeSingleQuoted(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\'' || r == '"' || r == '`' || r == '$' || r == 0 {
			return ' '
		}
		return r
	}, text)
}

// buildAgentCommand assembles the full delivery sequence the host executor
// launches: clone as the GitHub App (repomesh-bot), run the coding agent on
// the real requirement, commit, push a delivery branch and open the pull
// request. The App installation token is read from the cache file refreshed
// by repomesh-gh-token.timer — never embedded into the stored command.
//
// 依赖仓（depRepos）是同一计划里**其它仓库**的 owner/name：它们会被只读检出到本次
// 工作区的 _deps/ 下，并把路径写进 prompt。见 dependencyPromptSection 的说明。
// depSlug 是依赖仓在工作区里的目录名，必须与脚本里的 `tr / _` 逐字一致 ——
// 两处不一致时 prompt 会把 agent 指到一个不存在的目录，比没有这段还糟。
func depSlug(repoFullName string) string {
	return strings.ReplaceAll(repoFullName, "/", "_")
}

// safeRepoFullName 只放行 owner/name 这种形状。
//
// 为什么必须挡：这些名字会**拼进 shell 脚本**（`for DR in a/b c/d`）和
// `git clone .../$DR.git`。一个带空格或 $ 的名字不只是"显示难看"，而是把脚本拆成
// 别的命令。名字来自数据库里别人装 App 时登记的仓库，不是我们写的常量 —— 所以
// 形状不对就整个跳过（宁可少一个依赖仓，也不让脚本变形）。
func safeRepoFullName(repoFullName string) bool {
	if repoFullName == "" || strings.ContainsAny(repoFullName, " \t\r\n'\"`$\\;&|<>()*?[]{}") {
		return false
	}
	owner, name, found := strings.Cut(repoFullName, "/")
	return found && owner != "" && name != "" && !strings.Contains(name, "/")
}

// filteredDependencies 剔除形状不合法的仓库名并去重，保持调用方给的顺序。
func filteredDependencies(repoFullName string, depRepos []string) []string {
	out := make([]string, 0, len(depRepos))
	seen := map[string]bool{repoFullName: true}
	for _, repo := range depRepos {
		if !safeRepoFullName(repo) || seen[repo] {
			continue
		}
		seen[repo] = true
		out = append(out, repo)
	}
	return out
}

// dependencyPromptSection 把"你的输入不止本仓"这件事写进 prompt。
//
// 背景（2026-09-22 线上实测）：跨仓任务的 prompt 只说了 *Work only in this
// directory*，而依赖仓的约定文件根本不在这个目录里 —— agent 找不到，就退化成
// `grep -rl ... /` 和 `find / -name ...` **全盘搜索**：既慢（扫 400+ 目录），
// 又可能把**别的 attempt 未提交的中间产物**当成契约原文读进来（比找不到更糟）。
//
// 所以这里同时说清两件事：依赖仓**已经在你手上**（路径逐条列出），以及
// **不要全盘搜索**（工作区就是全部输入；真缺东西就如实说缺，不要猜）。
// 没有依赖仓时也保留"不要全盘搜索"这一句 —— 单仓任务同样不该去翻文件系统。
func dependencyPromptSection(repoFullName string, depRepos []string) string {
	deps := filteredDependencies(repoFullName, depRepos)
	var body strings.Builder
	if len(deps) > 0 {
		body.WriteString("## 依赖仓库（只读检出，已经在你手上）\n\n")
		body.WriteString("本次计划还涉及以下仓库，它们的当前 main 已经检出在你的工作区里：\n\n")
		for _, repo := range deps {
			body.WriteString("- " + repo + " -> ../_deps/" + depSlug(repo) + "\n")
		}
		body.WriteString("\n它们只是**只读参考**：可以读、可以对照，但不要修改、不要提交 —— 交付只针对 " +
			repoFullName + " 这一个仓库。\n")
		body.WriteString("若某个仓库没出现（检出失败），工作区里的 _deps/UNAVAILABLE.txt 会写明是哪一个。\n\n")
	}
	body.WriteString("## 不要全盘搜索\n\n")
	if len(deps) > 0 {
		body.WriteString("你的工作区（当前目录，以及上面列出的 ../_deps/ 目录）就是你的**全部输入**。\n")
	} else {
		body.WriteString("你的工作区（当前目录）就是你的**全部输入**。\n")
	}
	body.WriteString("不要用 find /、grep -r / 或任何以根目录为起点的搜索去找文件：那既慢，" +
		"又可能把别的任务未提交的中间产物当成约定原文读进来。\n")
	body.WriteString("确实需要的东西不在工作区里时，就在交付说明里写清**缺什么、你在哪里找不到**，不要猜。\n\n")
	return body.String()
}

// depCheckoutScript 生成"把依赖仓只读检出到 $PWD/_deps/<slug>"的那段 shell。
//
// 三条约束（都在这个文件别处踩过）：
//   - 整段脚本被 bash -c '...' 包着 —— 脚本内**不能出现单引号**；
//   - clone / fetch / worktree add 必须在 **per-repo 的 flock 里**：同一个依赖仓
//     会被多个并发 attempt 同时检出，两个 clone 撞在同一个目录上必然坏一个；
//   - 检出失败**不能杀掉整条交付**（那只是少了一份参考），但必须**留下名字**，
//     否则 prompt 里列了路径、目录却不存在，agent 又会退回去全盘搜索。
func depCheckoutScript(repoFullName string, depRepos []string) string {
	deps := filteredDependencies(repoFullName, depRepos)
	if len(deps) == 0 {
		return ""
	}
	var script strings.Builder
	script.WriteString("DEPS=\"$PWD/_deps\"\n")
	script.WriteString("mkdir -p \"$DEPS\"\n")
	script.WriteString("for DR in " + strings.Join(deps, " ") + "; do\n")
	script.WriteString("  DS=$(printf %s \"$DR\" | tr / _)\n")
	script.WriteString("  DB=$(dirname \"$PWD\")/_bases/$DS\n")
	script.WriteString("  ( flock 9\n")
	script.WriteString("    if [ ! -d \"$DB/.git\" ]; then git clone --depth 5 \"https://x-access-token:$T@github.com/$DR.git\" \"$DB\"; fi\n")
	script.WriteString("    git -C \"$DB\" remote set-url origin \"https://x-access-token:$T@github.com/$DR.git\"\n")
	script.WriteString("    git -C \"$DB\" fetch --depth 5 origin main\n")
	script.WriteString("    git -C \"$DB\" worktree prune\n")
	script.WriteString("    rm -rf \"$DEPS/$DS\"\n")
	script.WriteString("    git -C \"$DB\" worktree add --detach --force \"$DEPS/$DS\" FETCH_HEAD\n")
	script.WriteString("  ) 9>\"$(dirname \"$PWD\")/.lock-$DS\" || echo \"$DR checkout failed\" >> \"$DEPS/UNAVAILABLE.txt\"\n")
	script.WriteString("done\n")
	return script.String()
}

func buildAgentCommand(agentKind, model, instruction, repoFullName, attemptID, issueTitle, issueID, skillContent string, depRepos []string) (string, error) {
	requirement := strings.TrimSpace(instruction)
	if requirement == "" {
		requirement = "Complete the assigned task in this repository. Implement the requirement and run the existing checks."
	}
	prompt := "## Execution workspace contract\n\n" +
		"Repository: " + repoFullName + "\n" +
		"The current directory is the prepared git worktree for this repository. Work only in this directory.\n" +
		"Do not run git clone, git init, git commit, git push, gh pr create, or create pull requests. The platform owns delivery and will commit, push, and open the PR after you finish.\n\n" +
		dependencyPromptSection(repoFullName, depRepos) +
		"## Requirement\n\n" + requirement
	if strings.TrimSpace(skillContent) != "" {
		prompt = "## 你的技能（技能库原文）\n\n" + skillContent + "\n\n---\n\n" + prompt
	}
	prompt = sanitizeSingleQuoted(prompt)
	safeTitle := sanitizeSingleQuoted(issueTitle)
	if len([]rune(safeTitle)) > 60 {
		safeTitle = string([]rune(safeTitle)[:60])
	}
	var agentLine string
	switch agentKind {
	case "codex_cli":
		// Prompt travels in DOUBLE quotes: the whole script is wrapped in one
		// outer single-quote pair, and any inner single quote would close it
		// early — silently truncating the delivery sequence after the agent.
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"%s\"", model, prompt)
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"%s\" --model %s --dangerously-skip-permissions", prompt, model)
	case "dsh":
		// AgentTeams 原生 DeepSeek Harness（实验性）。DSH CLI 直接调 DeepSeek API，
		// 不经过 codex CLI → MiniMax 间接层。API key 走 DEEPSEEK_API_KEY 环境变量。
		// 命令模板可通过 REPOMESH_DSH_COMMAND 覆盖（%s 为 prompt 占位符），
		// 便于适配不同 DSH 版本的 CLI 接口而不重新编译。
		//
		// ⚠️ 2026-09-20 兼容性验证实测（这条模板此前是**错的**）：
		// DSH 0.1.1-rc.2 的真实 CLI 是
		//     dsh [options] [command] [args...]
		//       --profile <name>   boot the profile under $DSH_HOME/profiles
		//       web | plugin       两个子命令
		//     一次性任务：dsh --profile headless "任务文本"
		// 而旧模板写的 `dsh run --model %s --prompt "%s" --auto-approve` 里
		// **run / --model / --prompt / --auto-approve 四个东西都不存在** ——
		// 真跑立刻 `error: --profile <name> is required`。
		// 也就是说"把 codex 换成 DSH"这件事，在命令这一层从来没通过过。
		// 现在按实测形状改对（模型由 profile 配置决定，不再从命令行传）。
		dshCmd := os.Getenv("REPOMESH_DSH_COMMAND")
		if dshCmd == "" {
			dshCmd = "dsh --profile headless \"%s\""
		}
		// 占位符只有一个（任务文本）。旧模板有两个（model + prompt），
		// 所以覆盖模板的老用法也会在这里被显式拒绝，而不是静默少一个参数。
		if strings.Count(dshCmd, "%s") == 1 {
			agentLine = fmt.Sprintf(dshCmd, prompt)
		} else {
			agentLine = fmt.Sprintf(dshCmd, model, prompt)
		}
	default:
		return "", fmt.Errorf("coordinator: unsupported agent kind %q", agentKind)
	}
	// The whole sequence travels as ONE argv token (bash -c '...'); the
	// executor's quote-aware splitter restores it intact. Inside the script
	// only double quotes appear, so no shell quoting escapes the single pair.
	script := "set -e\n" +
		"T=${REPOMESH_GH_TOKEN:?missing installation token}\n" +
		"R=" + repoFullName + "\n" +
		"B=repomesh/auto-" + attemptID + "\n" +
		// 每个 task 一个**自己的 git worktree**（2026-09-20 用户裁定 + 当天修正）：
		// 每个仓库共享一份**浅基础克隆**（只做 clone/fetch），每个 attempt 在自己的
		// 工作区里 worktree add 一棵新树 —— 隔离性与"每次整仓 clone"一样（各自的
		// HEAD 与工作区互不影响），但省掉每次的整仓下载。
		//
		// 修正前这里把 worktree 建在 `$BASE/repo`（基础克隆**里面**），于是所有
		// attempt 共用同一棵树、互相踩，而且测试 agent 写的 test-evidence.json 落在
		// 基础目录下，executor 按 attempt 工作区去找 → 单点验收记录恒 0 条。
		// 现在 worktree 落在 `$PWD/repo`（本次 attempt 的工作区）—— BASE 只读用于
		// fetch/worktree add。
		//
		// BASE 上的 clone/fetch/prune/worktree add 用一把 per-repo 的 flock 串起来：
		// 同一仓库的并发 attempt 不再互相打断 git 的索引/引用写。锁只护这几条命令，
		// 不护 agent 执行段（否则同仓库的任务会被串行化）。
		// 注意：整段脚本被 bash -c '...' 包着，脚本内不能出现单引号。
		"SLUG=$(printf %s \"$R\" | tr / _)\n" +
		"BASE=$(dirname \"$PWD\")/_bases/$SLUG\n" +
		"WORK=\"$PWD/repo\"\n" +
		"mkdir -p \"$(dirname \"$BASE\")\"\n" +
		"if [ ! -d \"$BASE/.git\" ]; then git clone --depth 5 \"https://x-access-token:$T@github.com/$R.git\" \"$BASE\"; fi\n" +
		"( flock 9\n" +
		"  git -C \"$BASE\" remote set-url origin \"https://x-access-token:$T@github.com/$R.git\"\n" +
		"  git -C \"$BASE\" fetch --depth 5 origin main\n" +
		"  git -C \"$BASE\" worktree prune\n" +
		"  rm -rf \"$WORK\"\n" +
		"  git -C \"$BASE\" worktree add --detach --force \"$WORK\" FETCH_HEAD\n" +
		") 9>\"$(dirname \"$BASE\")/.lock-$SLUG\"\n" +
		// 依赖仓的只读检出放在**本次 attempt 的工作区**里（$PWD/_deps），
		// 不在 $WORK 里面 —— 交付的 `git add -A -- .` 只覆盖 repo/，
		// 所以参考树不会被误提交进交付分支。
		depCheckoutScript(repoFullName, depRepos) +
		"cd \"$WORK\"\n" +
		"git config user.name \"repomesh-bot[bot]\"\n" +
		"git config user.email \"repomesh-bot@users.noreply.github.com\"\n" +
		agentLine + "\n" +
		// bug B（2026-09-20）：交付前先清掉构建产物 —— 线上出现过"0 insertions,
		// 0 deletions"的空提交里只躺着一个 tests/__pycache__/*.pyc。然后**确认真的
		// 还有改动**：没有就明确失败（exit 3 + REPO_DELIVERY_EMPTY），而不是推一个
		// 空分支、开一个空 PR 让下游以为交付成功了。
		"find . -type d -name __pycache__ -prune -exec rm -rf {} +\n" +
		"find . -type f -name \"*.pyc\" -delete\n" +
		"rm -rf .pytest_cache\n" +
		// 交付前回滚 **lockfile 噪声**（2026-09-21 线上实测）。
		//
		// 实测：agent 跑一次 npm/pnpm install 就会把 package-lock.json 整个重写 ——
		// 那次 PR 是 +31,442 / -23,402 行，而任务真实改动只有 11 行（新增
		// src/health/probe.ts 6 行 + 测试 5 行）。评审的人从 diff 里看不出"到底改了什么"，
		// 而 PR 的体积本身就会被当成交付质量问题。
		//
		// 判据：**清单文件没变** → 说明依赖不是这次任务的一部分 → 把 lockfile 回滚掉。
		// 清单变了（package.json / pnpm-workspace.yaml）则保留：那可能是任务要求升级依赖，
		// 此时 lockfile 的改动是真的交付物，不能丢。
		// 只回滚**已跟踪**的 lockfile（`git checkout --` 对不存在的路径会报错，故 || true）：
		// 不删未跟踪文件 —— 那可能是仓库里本来就该有的东西，删了就成破坏。
		"if git diff --quiet -- package.json pnpm-workspace.yaml; then\n" +
		"  git checkout -- package-lock.json pnpm-lock.yaml yarn.lock npm-shrinkwrap.json 2>/dev/null || true\n" +
		"fi\n" +
		// 2026-09-20 线上实测：上面那两条 find **没能**把产物挡在提交之外（PR 里
		// 依然出现 src/__pycache__/*.pyc）。所以再加一道**不依赖顺序**的闸：
		// git add 直接按 pathspec 排除构建产物 —— 就算文件还在工作区，也进不了提交。
		// 这不是"眼不见为净"：产物本来就不该进交付，交付物要能被人看懂。
		// 注意：整段脚本被 bash -c '...' 包着，所以 pathspec 只能用**双引号**
		// （脚本内出现单引号会让外层引号提前闭合，交付序列被静默截断）。
		"git add -A -- . \":(exclude)**/__pycache__/**\" \":(exclude)**/*.pyc\" \":(exclude)**/*.pyo\" \":(exclude)**/.pytest_cache/**\"\n" +
		"if git diff --cached --quiet; then echo REPO_DELIVERY_EMPTY=1; exit 3; fi\n" +
		"git commit -m \"RepoMesh delivery " + issueID + ": " + safeTitle + "\"\n" +
		// A2（2026-09-20 线上实测）：交付分支是**派工时**的 main 拉出来的，而 agent 要跑
		// 几分钟 —— 这期间别的交付可能已经合进 main。于是"先合的那个 PR"会让后合的
		// **必然冲突**（实测：同一个 e2e 仓库两条任务都改 src/pricing.py，#25 一合进
		// main，#26 立刻变成 dirty，界面上点合并就回 405 "Pull request has merge
		// conflicts"）。这里在推送**之前**把最新 main rebase 进来：PR 因此总是长在最新
		// main 上，合并时不会再有冲突。
		//
		// rebase 真冲突（同一处被两边改了）就**如实失败**：不许硬推、不许挑一边、
		// 也不许悄悄放弃自己的改动 —— 那种情况需要人（或协调 agent）来判。
		"git fetch --depth 5 origin main\n" +
		"if ! git rebase FETCH_HEAD >/dev/null 2>&1; then\n" +
		"  git rebase --abort >/dev/null 2>&1 || true\n" +
		"  echo REPO_DELIVERY_CONFLICT=1\n" +
		"  exit 4\n" +
		"fi\n" +
		"git push origin HEAD:refs/heads/$B\n" +
		"curl -sf -X POST -H \"Authorization: Bearer $T\" -H \"Accept: application/vnd.github+json\" " +
		"https://api.github.com/repos/$R/pulls " +
		"-d \"{\\\"title\\\":\\\"RepoMesh: " + safeTitle + "\\\",\\\"head\\\":\\\"$B\\\",\\\"base\\\":\\\"main\\\",\\\"body\\\":\\\"RepoMesh automated delivery for issue " + issueID + "\\\"}\" > pr.json\n" +
		"echo REPO_PR_CREATED=$R:$B\n" +
		// 2026-09-20：把 PR 链接也打出来。合并闸门与合并动作都要**PR 编号**
		//（GitHub 的合并接口按编号调），只有 repo:branch 是合不了的。
		// 模式里不含引号：整段脚本被单引号包着，脚本内不能再出现单引号。
		"PR_PATH=$(grep -o \"github.com/[^/]*/[^/]*/pull/[0-9]*\" pr.json | head -1)\n" +
		"echo REPO_PR_URL=https://$PR_PATH"
	return "bash -c '" + script + "'", nil
}

// buildTestCommand assembles the test-agent sequence. It runs in the SAME
// attempt workspace the development agent just used (so the delivered change
// is right there under repo/), inspects that change, writes a test script for
// it and runs it — the single-point acceptance the delivery chain was missing.
//
// Before this, every plan step was assignee_role='worker' and the only
// "verification" was a human reading the pull request: agent_runs carried no
// test evidence at all (contract's test_command/test_results stayed null/[]).
// The exit code of this run is the platform's first machine-checkable signal.
func buildTestCommand(agentKind, model, instruction, repoFullName, attemptID, issueTitle, testSkillContent string) (string, error) {
	requirement := strings.TrimSpace(instruction)
	if requirement == "" {
		requirement = strings.TrimSpace(issueTitle)
	}
	if requirement == "" {
		requirement = "the delivered change"
	}
	// 单引号会让外层 bash -c '...' 提前闭合（git 段静默丢失、exit 0 假成功），
	// 与 buildAgentCommand 同一道 sanitize。
	requirement = sanitizeSingleQuoted(requirement)
	// 2026-09-20：测试结论此前只留在 agent 的 stdout 散文里，平台侧只剩一个退出码。
	// 现在要求它把结论写成**机器可读的产物文件**（与规划 agent 写
	// planning-artifact.json 同一套做法），executor 在 run 退出后读回、入库 ——
	// 单点验收从此可查：脚本是哪个、跑了什么命令、退出码、结论是什么。
	testPrompt := "You are the test agent for this repository. " +
		"Requirement: " + requirement + ". " +
		"Inspect the uncommitted change the development agent just delivered (git status --short and git diff). " +
		// 与开发 run 同一份工作区：依赖仓的只读检出（../_deps/）还在原地，测试可以
		// 拿它对照跨仓约定。但**不要**去全盘搜索 —— 理由同 dependencyPromptSection。
		"The workspace may also contain ../_deps/ with read-only checkouts of the other repositories in this plan; " +
		"read them for reference, never modify them. " +
		"Do not search the whole filesystem (no find / or grep -r /). " +
		"Write a test script that verifies the requirement and run it. " +
		"Then write the result to a file named " + execution.TestEvidenceFile + " in the current directory, " +
		"as this exact JSON shape and nothing else: " +
		`{"script":"<path of the test script you wrote>","command":"<the exact command you ran>",` +
		`"exit_code":<integer>,"passed":<true|false>,"summary":"<one line, what you actually observed>"}. ` +
		"If the change does not satisfy the requirement, set passed=false and say so in summary " +
		"instead of reporting success. Do not invent results."
	if strings.TrimSpace(testSkillContent) != "" {
		testPrompt = "## 你的技能（技能库原文）\n\n" + testSkillContent + "\n\n---\n\n" + testPrompt
	}
	testPrompt = sanitizeSingleQuoted(testPrompt + typesafe.RuntimePrompt)
	var agentLine string
	switch agentKind {
	case "codex_cli":
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"%s\"", model, testPrompt)
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"%s\" --model %s --dangerously-skip-permissions", testPrompt, model)
	case "dsh":
		// 与上面开发 agent 同一处修正（2026-09-20 兼容性验证）：DSH 0.1.1-rc.2
		// 没有 run/--model/--prompt/--auto-approve，一次性任务就是
		// `dsh --profile headless "<任务文本>"`。
		dshCmd := os.Getenv("REPOMESH_DSH_COMMAND")
		if dshCmd == "" {
			dshCmd = "dsh --profile headless \"%s\""
		}
		if strings.Count(dshCmd, "%s") == 1 {
			agentLine = fmt.Sprintf(dshCmd, testPrompt)
		} else {
			agentLine = fmt.Sprintf(dshCmd, model, testPrompt)
		}
	default:
		return "", fmt.Errorf("coordinator: unsupported test agent kind %q", agentKind)
	}
	script := "set -e\n" +
		"R=" + repoFullName + "\n" +
		// 测试 agent 跑在**开发 run 的那棵 worktree** 里（executor 把两条 run 的
		// workspace 都指向开发 attempt 的工作区），所以这里直接进本次工作区的
		// repo/ —— 被交付的改动就在眼前。修正前它跟着基础克隆走
		// （`$BASE/repo`），而那棵树是所有 attempt 共用的。
		"cd \"$PWD/repo\"\n" +
		typesafe.PrepareScript +
		agentLine + "\n" +
		"echo REPO_TESTS_DONE=" + repoFullName + ":" + attemptID
	return "bash -c '" + script + "'", nil
}

// planSiblingRepositories 列出**同一计划里其它仓库**的 owner/name（排除本次交付的
// 那一个），按名字排序保证同一条任务每次生成的脚本逐字相同。
//
// 返回空列表的三种情形都是"本来就没有跨仓输入"，不是错误：
//
//	· 任务不属于任何计划（plan_id 为空）—— 子查询给 NULL，条件不成立；
//	· 计划只有一个仓库 —— 排除掉自己就空了；
//	· 计划的其它任务没有仓库绑定 —— JOIN 不上。
//
// 刻意**不走** public.tasks.repository_id：那个字段是 text，而权威绑定在
// task_repository_scopes（ReserveForTask 的主查询也走它）。同一件事两个来源，
// 迟早有一个是过期的 —— 这里跟权威那一个走。
func planSiblingRepositories(ctx context.Context, tx pgx.Tx, taskID, selfRepoFullName string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ro.owner || '/' || ro.name
		FROM public.tasks t2
		JOIN public.task_repository_scopes s2 ON s2.task_id = t2.id
		JOIN repomesh_projects.repositories ro ON ro.id = s2.repository_id
		WHERE t2.plan_id = (SELECT plan_id FROM public.tasks WHERE id = $1::uuid)
		  AND t2.id <> $1::uuid
		  AND ro.owner || '/' || ro.name <> $2
		ORDER BY 1`, taskID, selfRepoFullName)
	if err != nil {
		return nil, fmt.Errorf("coordinator: dependency repositories for task %s: %w", taskID, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("coordinator: dependency repositories for task %s: %w", taskID, err)
		}
		out = append(out, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coordinator: dependency repositories for task %s: %w", taskID, err)
	}
	return out, nil
}

// ReserveForTask registers the worker, one launch-verified attempt bound to
// the task's real issue, and the pending agent run whose command the executor
// claims. The attempt is inserted directly in launch_verified: the
// coordinator is the formal state writer (0020), the worker row it points at
// was just upserted with a live heartbeat, and the claim JOIN only matches
// launch_verified/running attempts.
func (l *coordinatorLedger) ReserveForTask(ctx context.Context, workerID, taskID, agentKind, title, instruction string) (string, error) {
	if _, err := l.pool.Exec(ctx, `INSERT INTO repomesh_execution.workers (id, host, kind, heartbeat_at)
		VALUES ($1,'coordinator','host_executor', clock_timestamp())
		ON CONFLICT (id) DO UPDATE SET heartbeat_at=clock_timestamp(), retired_at=NULL`, workerID); err != nil {
		return "", fmt.Errorf("coordinator: worker register failed: %w", err)
	}
	attemptID, err := newRunID("att_dag_")
	if err != nil {
		return "", err
	}
	// One short transaction binds everything: the task's real project/issue/
	// pinned-revision/repository feed the attempt and the command (the FK and
	// the configuration-match trigger both check real rows, so unresolved
	// placeholders abort here and dispatch defers to the next tick), and the
	// pending agent run commits in the same transaction so the executor can
	// never observe a half dispatch.
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("coordinator: reserve begin failed: %w", err)
	}
	defer tx.Rollback(ctx)
	var projectID, issueID, revision, repoFullName, issueTitle, configuredKind, configuredModel string
	err = tx.QueryRow(ctx, `SELECT t.project_id::text, scope.issue_id, i.initial_configuration_revision,
		       r.owner || '/' || r.name, i.title
		       , COALESCE(a.agent_kind, ''), COALESCE(a.model, '')
		FROM public.tasks t
		JOIN public.task_repository_scopes scope ON scope.task_id=t.id AND scope.project_id=t.project_id::text
		JOIN repomesh_projects.repositories r ON r.id=scope.repository_id
		JOIN repomesh_issues.issues i ON i.project_id = scope.project_id AND i.id = scope.issue_id
		LEFT JOIN repomesh_projects.agent_settings a ON a.project_id = t.project_id::text
		WHERE t.id::text=$1 LIMIT 1`, taskID).
		Scan(&projectID, &issueID, &revision, &repoFullName, &issueTitle, &configuredKind, &configuredModel)
	if err != nil {
		return "", fmt.Errorf("coordinator: task %s has no confirmed Issue repository binding", taskID)
	}
	// 项目级智能体配置（/app/ 项目页保存）覆盖部署默认：CLI 种类与模型。
	if configuredKind == "codex_cli" || configuredKind == "claude_cli" {
		agentKind = configuredKind
	}
	if configuredModel == "" {
		configuredModel = "MiniMax-M2"
	}
	// Fetch the active skill content for the worker role (this task's executor).
	sb := newSkillBridge(l.pool)
	workerSkill, _ := sb.ForRole(ctx, "worker")

	// 依赖仓：同一计划里**其它仓库**。跨仓任务要引用别的仓的约定原文，而 agent 手上
	// 只有本仓 —— 2026-09-22 线上实测它因此退化成全盘搜索。这里把兄弟仓列出来，
	// 由交付脚本只读检出到本次工作区的 _deps/ 下（见 depCheckoutScript）。
	// 查不到（单仓计划 / 任务没有计划）就是空列表：没有跨仓输入，工作区里也不该多目录。
	depRepos, depErr := planSiblingRepositories(ctx, tx, taskID, repoFullName)
	if depErr != nil {
		return "", depErr
	}

	command, err := buildAgentCommand(agentKind, configuredModel, instruction, repoFullName, attemptID, issueTitle, issueID, workerSkill.Content, depRepos)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		attemptID, projectID, issueID, workerID, revision); err != nil {
		return "", fmt.Errorf("coordinator: attempt insert failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events
		(attempt_id, sequence, kind, source)
		VALUES ($1, 1, 'launch_verified', 'coordinator')
		ON CONFLICT DO NOTHING`, attemptID); err != nil {
		return "", fmt.Errorf("coordinator: attempt event insert failed: %w", err)
	}
	runID, err := newRunID("run_dag_")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, repo_full_name)
		VALUES ($1,$2,$3,$4,$5,$6,'pending',$7)`,
		runID, attemptID, agentKind, command,
		"/opt/repomesh/workspaces/"+attemptID, taskID, repoFullName); err != nil {
		return "", fmt.Errorf("coordinator: agent run insert failed: %w", err)
	}
	// 双派工：测试 run 挂在**独立的 attempt** 上。
	// agent_runs_one_live_per_attempt 是 (attempt_id) 上 WHERE state IN
	// ('pending','running') 的部分唯一索引——同一 attempt 只容得下一条 live run。
	// 把两条 run 放同一 attempt 会被 23505 拒绝，且因为同一事务而让整次派工回滚
	// （连开发 run 一起消失，DAG 永远推不动）。这正是 DispatchDual 原本就建两个
	// attempt 的原因。
	// workspace 仍指向**开发 run 的工作区**：被交付的改动就在那个目录的 repo/ 下，
	// 测试 agent 因此看到真实产物，而不是一份重新克隆的干净树。
	// 两条 run 仍在同一事务插入，created_at 相同，所以 ClaimAgentLaunch 用
	// (agent_kind='test_agent') 作 tiebreaker 保证先开发后测试。
	// agent_kind='test_agent' 已由 agent_runs_agent_kind_check 允许，无需迁移。
	testAttemptID, err := newRunID("att_dag_")
	if err != nil {
		return "", err
	}
	// reservation_generation 取 MAX+1：开发 attempt 刚在同一事务里插入，因此测试
	// attempt 会拿到 +1，不会撞 (project_id,issue_id,reservation_generation) 唯一键
	// （撞了会 DO NOTHING 静默不插，随后 agent_runs 的外键直接失败）。
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		testAttemptID, projectID, issueID, workerID, revision); err != nil {
		return "", fmt.Errorf("coordinator: test attempt insert failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events
		(attempt_id, sequence, kind, source)
		VALUES ($1, 1, 'launch_verified', 'coordinator')
		ON CONFLICT DO NOTHING`, testAttemptID); err != nil {
		return "", fmt.Errorf("coordinator: test attempt event insert failed: %w", err)
	}
	testRunID, err := newRunID("run_test_")
	if err != nil {
		return "", err
	}
	// Test responsibilities are explicit; a role-only lookup could select a
	// different project's latest Worker binding or replace the test capability.
	testSkillContent := skill.SeedDoc("test-review") + "\n\n" + skill.SeedDoc("self-test")
	testCommand, err := buildTestCommand(agentKind, configuredModel, instruction, repoFullName, attemptID, issueTitle, testSkillContent)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,'test_agent',$3,$4,$5,'pending')`,
		testRunID, testAttemptID, testCommand,
		"/opt/repomesh/workspaces/"+attemptID, taskID); err != nil {
		return "", fmt.Errorf("coordinator: test run insert failed: %w", err)
	}
	if err := enqueueTypeSafeReview(ctx, tx, projectID, issueID, revision, workerID, taskID, attemptID, agentKind, configuredModel, instruction+"\n"+issueTitle); err != nil {
		return "", fmt.Errorf("coordinator: code review run insert failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("coordinator: reserve commit failed: %w", err)
	}
	return attemptID, nil
}
