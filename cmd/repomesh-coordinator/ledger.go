package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

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
func buildAgentCommand(agentKind, model, instruction, repoFullName, attemptID, issueTitle, issueID, skillContent string) (string, error) {
	requirement := strings.TrimSpace(instruction)
	if requirement == "" {
		requirement = "Complete the assigned task in this repository. Implement the requirement and run the existing checks."
	}
	prompt := "## Execution workspace contract\n\n" +
		"Repository: " + repoFullName + "\n" +
		"The current directory is the prepared git worktree for this repository. Work only in this directory.\n" +
		"Do not run git clone, git init, git commit, git push, gh pr create, or create pull requests. The platform owns delivery and will commit, push, and open the PR after you finish.\n\n" +
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

	command, err := buildAgentCommand(agentKind, configuredModel, instruction, repoFullName, attemptID, issueTitle, issueID, workerSkill.Content)
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
