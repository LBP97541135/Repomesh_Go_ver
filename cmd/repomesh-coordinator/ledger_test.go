package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestScriptSyntaxIsValidBash 把生成的三段脚本（交付/测试/集成）交给 `bash -n`
// 做语法自检。
//
// 为什么需要：这些脚本是**拼字符串**拼出来的，一个括号或引号错位在单测里看不出来
// （字符串包含断言照样通过），却会让整条交付链在服务器上静默失败 —— 线上出现过
// "外层单引号提前闭合、agent 之后的 commit/push/开 PR 全没了、退出码还是 0"。
// 没有 bash 的环境（Windows 开发机）跳过，不假装通过。
func TestScriptSyntaxIsValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("没有 bash，跳过语法自检")
	}
	// Windows 上 LookPath 常能找到一个 **WSL 的 bash 壳**，而它背后没有真的 /bin/bash
	// （报 "execvpe(/bin/bash) failed: No such file or directory"）。先拿一段必然合法的
	// 脚本试一次：跑不起来就说明这台机器没有可用的 bash，跳过 —— 不制造假失败。
	probe := exec.Command(bash, "-n")
	probe.Stdin = strings.NewReader("set -e\necho ok\n")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("这台机器的 bash 不可用（%v：%s），跳过语法自检", err, strings.TrimSpace(string(out)))
	}
	delivery, err := buildAgentCommand("codex_cli", "MiniMax-M2",
		"把运费改成满 900 免运费", "owner/name", "att_1", "标题", "iss_1", "技能原文", nil)
	if err != nil {
		t.Fatal(err)
	}
	testCmd, err := buildTestCommand("codex_cli", "MiniMax-M2", "add a file", "owner/name", "att_1", "title", "")
	if err != nil {
		t.Fatal(err)
	}
	integration := buildIntegrationCommand("codex_cli", "MiniMax-M2", "owner/name", nil)
	for name, command := range map[string]string{
		"delivery":    delivery,
		"test":        testCmd,
		"integration": integration,
	} {
		inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(inner)
		cmd.Env = append(os.Environ(), "REPOMESH_GH_TOKEN=x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s 脚本语法错误：%v\n%s\n---\n%s", name, err, out, inner)
		}
	}
}

// 交付脚本的硬约束（都在线上踩过）：
//  1. 整段脚本被 bash -c '...' 包着 —— 脚本内**不能出现单引号**，否则外层引号提前
//     闭合，交付序列会在 agent 之后被静默截断（commit/push/开 PR 全没了）；
//  2. 每个 task 必须走**一棵自己的 worktree**（2026-09-20 用户裁定），而不是每次整仓 clone；
//  3. 这棵树必须落在**本次 attempt 的工作区**（$PWD/repo）里，不能建在共享基础克隆
//     里面（$BASE/repo）—— 否则所有 attempt 共用一棵树互相踩，而且测试 agent 写的
//     test-evidence.json 会落在基础目录下，executor 按 attempt 工作区找 → 单点验收恒 0 条；
//  4. 交付前要清掉 __pycache__/*.pyc，并且**真的没有改动时明确失败**，不许开空 PR。
func TestBuildAgentCommandKeepsScriptQuotableAndUsesWorktree(t *testing.T) {
	command, err := buildAgentCommand("codex_cli", "MiniMax-M2",
		"把运费改成满 900 免运费", "owner/name", "att_1", "满900免运费", "iss_1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(command, "bash -c '") || !strings.HasSuffix(command, "'") {
		t.Fatalf("命令必须整段被单引号包住：%s", command)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("脚本内出现单引号：外层引号会提前闭合，交付序列被截断")
	}
	for _, want := range []string{
		"worktree add --detach --force \"$WORK\" FETCH_HEAD",
		"_bases/$SLUG",
		"WORK=\"$PWD/repo\"",
		"rm -rf \"$WORK\"",
		"git push origin HEAD:refs/heads/$B",
		"find . -type d -name __pycache__ -prune -exec rm -rf {} +",
		"find . -type f -name \"*.pyc\" -delete",
		"if git diff --cached --quiet; then echo REPO_DELIVERY_EMPTY=1; exit 3; fi",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("脚本缺少 %q：%s", want, inner)
		}
	}
	// 2026-09-22：agent 的退出码不能当成交付的判据。
	//
	// 线上实测：`定义折扣规则类型系统 (rules.ts)` 重派 4 次全 exit=1，报告都写
	// "未产出可交付的改动"；可工作区里 4 个文件已经 staged、测试 agent 也验过
	// （passed=true，还做了 mutation 自检）。真相是脚本第一行的 `set -e` 让 agent
	// 非 0 退出**立刻中止整段脚本** —— 交付段（commit/push/开 PR）一行都没执行。
	// 判据本来就该是"有没有改动"（下面那条 REPO_DELIVERY_EMPTY），不是"agent 退没退 0"。
	agentAt := strings.Index(inner, "set +e\n")
	agentExitAt := strings.Index(inner, "REPOMESH_AGENT_EXIT=$?")
	guardAt := strings.Index(inner, "REPO_DELIVERY_EMPTY=1")
	if agentAt < 0 || agentExitAt < 0 {
		t.Fatalf("交付脚本没有把 agent 段从 set -e 里摘出来（agent 一非 0 退出，交付段就整段丢失）：\n%s", inner)
	}
	if !(agentAt < agentExitAt && agentExitAt < guardAt) {
		t.Fatalf("set +e / 记录退出码 / 交付判据 的顺序不对：\n%s", inner)
	}
	if !strings.Contains(inner, "echo REPO_AGENT_EXIT=$REPOMESH_AGENT_EXIT") {
		t.Fatalf("agent 的退出码没有落进日志（交付成功了也要能查它当时退了几）：\n%s", inner)
	}
	if strings.Contains(inner, "git clone --depth 5 https://x-access-token:$T@github.com/$R.git repo\n") {
		t.Fatal("还在用整仓 clone 建工作区（应改为共享基础克隆 + worktree add）")
	}
	if !strings.Contains(inner, "cd \"$WORK\"") {
		t.Fatalf("必须进入本次 attempt 自己的 worktree：%s", inner)
	}
	// 反向断言：树不能建在共享基础克隆里面。
	for _, forbidden := range []string{"$BASE/repo", "cd \"$BASE\""} {
		if strings.Contains(inner, forbidden) {
			t.Fatalf("worktree 又建到共享基础克隆里了（%s）：%s", forbidden, inner)
		}
	}
}

func TestBuildAgentCommandIncludesSkillContent(t *testing.T) {
	skillDoc := "You are a task execution worker. Follow the acceptance criteria."
	command, err := buildAgentCommand("codex_cli", "MiniMax-M2",
		"do the thing", "owner/name", "att_1", "title", "iss_1", skillDoc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "task execution worker") {
		t.Fatal("技能原文没有注入 prompt")
	}
	if !strings.Contains(command, "The current directory is the prepared git worktree") ||
		!strings.Contains(command, "Do not run git clone") ||
		!strings.Contains(command, "platform owns delivery") {
		t.Fatalf("执行 prompt 缺少工作区契约：%s", command)
	}
}

func TestBuildIntegrationCommandUsesWorktree(t *testing.T) {
	command := buildIntegrationCommand("codex_cli", "MiniMax-M2", "owner/name", nil)
	if !strings.HasPrefix(command, "bash -c '") || !strings.HasSuffix(command, "'") {
		t.Fatalf("命令必须整段被单引号包住：%s", command)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("脚本内出现单引号：外层引号会提前闭合")
	}
	if !strings.Contains(inner, "worktree add --detach --force \"$WORK\" FETCH_HEAD") {
		t.Fatalf("集成脚本没有走 worktree：%s", inner)
	}
	if !strings.Contains(inner, "cd \"$WORK\"") || !strings.Contains(inner, `$(cat "$PROMPT")`) {
		t.Fatalf("集成脚本必须进入 worktree 并读取任务工作区的 prompt：%s", inner)
	}
	if strings.Contains(inner, "$BASE/repo") {
		t.Fatalf("集成 worktree 又建到共享基础克隆里了：%s", inner)
	}
}

func TestBuildTestCommandUsesAttemptWorktree(t *testing.T) {
	command, err := buildTestCommand("codex_cli", "MiniMax-M2", "add a file", "owner/name", "att_1", "title", "")
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	for _, want := range []string{
		`cd "$PWD/repo"`,
		"git status --short",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("测试脚本缺少 %q：%s", want, inner)
		}
	}
	if strings.Contains(inner, "$BASE") {
		t.Fatalf("测试脚本又跟着共享基础克隆走了：%s", inner)
	}
	if strings.Contains(inner, "\ncd repo\n") {
		t.Fatalf("测试脚本仍使用旧工作区路径：%s", inner)
	}
}

// 依赖仓进工作区（2026-09-22 用户裁定：跨仓任务不该让 agent 去全盘搜索）。
//
// 线上实测的病灶：需求要 agent 引用**另一个仓库**的约定文件，而 prompt 只说了
// "Work only in this directory" —— 文件不在这个目录里，agent 于是退化成
// `grep -rl ... /` 与 `find / -name ...`：扫 400+ 目录，还可能把别的 attempt
// 未提交的中间产物当成契约原文读进来。
//
// 修法是把同计划其它仓库**只读检出到本次工作区的 _deps/ 下**，并把路径写进 prompt。
// 这里锁住四件事：路径出现在 prompt、检出脚本真的建了它、**交付只针对本仓**、
// 以及形状不合法的仓库名不会把脚本拆坏。
func TestBuildAgentCommandMaterializesDependencyRepositories(t *testing.T) {
	deps := []dependencyRepo{
		{FullName: "LBP97541135/saleor-sdk"},
		{FullName: "LBP97541135/saleor-dashboard"},
	}
	command, err := buildAgentCommand("codex_cli", "MiniMax-M2",
		"把 sdk 的契约复制过来", "LBP97541135/saleor-app-template",
		"att_1", "跨仓契约", "iss_1", "", deps)
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("脚本内出现单引号：外层引号会提前闭合")
	}
	// 1) 检出段：两个依赖仓都被 clone/fetch/worktree add 到 $PWD/_deps/<slug>。
	for _, want := range []string{
		"DEPS=\"$PWD/_deps\"",
		"for ENTRY in LBP97541135/saleor-sdk: LBP97541135/saleor-dashboard:; do",
		"git -C \"$DB\" worktree add --detach --force \"$DEPS/$DS\" FETCH_HEAD",
		"9>\"$(dirname \"$PWD\")/.lock-$DS\"",
		"UNAVAILABLE.txt",
		"WANT=main",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("交付脚本缺少依赖检出片段 %q：\n%s", want, inner)
		}
	}
	// 2) 检出必须发生在**进 $WORK 之前**（那时 $PWD 还是本次 attempt 的工作区）。
	//    反了就会把 _deps 建进交付树里，参考仓会被当成交付物提交上去。
	if strings.Index(inner, "DEPS=\"$PWD/_deps\"") > strings.Index(inner, "cd \"$WORK\"") {
		t.Fatal("依赖检出排在 cd $WORK 之后：_deps 会被建进交付树里")
	}
	// 3) prompt 里逐条给出路径，并明说只读、只交付本仓。
	if !strings.Contains(inner, "../_deps/LBP97541135_saleor-sdk") {
		t.Fatalf("prompt 没有给出依赖仓路径：\n%s", inner)
	}
	if !strings.Contains(inner, "只读参考") {
		t.Fatalf("prompt 没有说明依赖仓是只读的：\n%s", inner)
	}
	// 4) 反全盘搜索那一句必须在 —— 这才是这条链要治的病。
	if !strings.Contains(inner, "不要全盘搜索") {
		t.Fatalf("prompt 缺少禁止全盘搜索的约束：\n%s", inner)
	}
}

// 没有依赖仓时：不该凭空多出 _deps 目录，但"不要全盘搜索"仍然要说 —— 单仓任务
// 同样不该去翻文件系统找输入。
func TestBuildAgentCommandWithoutDependenciesStillForbidsWholeFilesystemSearch(t *testing.T) {
	command, err := buildAgentCommand("codex_cli", "MiniMax-M2",
		"单仓任务", "owner/name", "att_1", "title", "iss_1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "_deps") {
		t.Fatalf("没有依赖仓却生成了 _deps 片段：\n%s", inner)
	}
	if !strings.Contains(inner, "不要全盘搜索") {
		t.Fatalf("prompt 缺少禁止全盘搜索的约束：\n%s", inner)
	}
}

// 形状不合法的仓库名**不能进脚本**：它会被拼进 `for DR in ...` 与 git clone 的 URL。
// 名字来自别人装 App 时登记的仓库，不是我们写的常量 —— 一个空格就能把脚本拆成
// 别的命令，一个 $ 就能让 shell 展开成别的东西。宁可少一个依赖仓。
func TestDependencyRepositoriesRejectUnsafeNames(t *testing.T) {
	got := filteredDependencies("owner/self", []dependencyRepo{
		{FullName: "owner/ok"},
		{FullName: "owner/self"},          // 自己：交付仓不该出现在依赖里
		{FullName: "owner/ok"},            // 重复，且这条没有 ref —— 不该把上面那条降级
		{FullName: ""},                    // 空
		{FullName: "noslash"},             // 没有 owner
		{FullName: "own er/name"},         // 空格会把 for 列表拆开
		{FullName: "owner/na$me"},         // $ 会被 shell 展开
		{FullName: "owner/na;me"},         // ; 会另起一条命令
		{FullName: "owner/a/b"},           // 多一段路径
		{FullName: "owner/back`tick"},     // 反引号是命令替换
		{FullName: "owner/quote'name"},    // 单引号会闭合外层 bash -c
		{FullName: "LBP97541135/another"}, // 合法，保留
	})
	want := []string{"owner/ok", "LBP97541135/another"}
	if len(got) != len(want) {
		t.Fatalf("过滤结果不对：得到 %v，期望 %v", got, want)
	}
	for index := range want {
		if got[index].FullName != want[index] {
			t.Fatalf("过滤结果不对：得到 %v，期望 %v", got, want)
		}
	}
}

// 交付分支要优先于 main，且 ref 本身也要过形状检查 —— 它会进
// `git fetch origin "$REF"`。同一个仓库出现多次时，**有 ref 的那条要赢**：
// 那是"同计划任务已经交付"这个更有信息量的来源，丢掉它就等于白白退回 main。
func TestDependencyRefsPreferDeliveryBranchAndRejectUnsafeRefs(t *testing.T) {
	got := filteredDependencies("owner/self", []dependencyRepo{
		{FullName: "owner/sdk", Ref: "repomesh/auto-att_dag_574f2c1e2cf5de810d34"},
		{FullName: "owner/sdk"},                                       // 同仓、无 ref：不该覆盖上面那条
		{FullName: "owner/dash", Ref: "main"},                         // 不是我们铸的分支名 → 退回 main
		{FullName: "owner/api", Ref: "repomesh/auto-att_1; rm -rf /"}, // 注入 → 退回 main
	})
	if len(got) != 3 {
		t.Fatalf("期望 3 条，得到 %v", got)
	}
	if got[0].Ref != "repomesh/auto-att_dag_574f2c1e2cf5de810d34" {
		t.Fatalf("有 ref 的那条被无 ref 的重复项盖掉了：%v", got[0])
	}
	if got[1].Ref != "" {
		t.Fatalf("非 repomesh/auto- 的 ref 没被挡下：%v", got[1])
	}
	if got[2].Ref != "" {
		t.Fatalf("带注入的 ref 没被挡下：%v", got[2])
	}
	// ref 必须真的写进脚本的 fetch，而不是只写在 prompt 里。
	script := depCheckoutScript("owner/self", got)
	if !strings.Contains(script, "for ENTRY in owner/sdk:repomesh/auto-att_dag_574f2c1e2cf5de810d34 owner/dash: owner/api:; do") {
		t.Fatalf("脚本里的依赖列表不对：\n%s", script)
	}
	// 按 ref 取、失败才退回 main；且**只在真的从分支退回时**才记那句话
	// （旧写法在 WANT 本来就是 main 时也会打印「取不到 main，退回 main」）。
	if !strings.Contains(script, "fetch --depth 5 origin \"$REF\"") {
		t.Fatalf("脚本没有按交付分支取：\n%s", script)
	}
	if strings.Contains(script, "取不到 $WANT，退回 main") {
		t.Fatalf("又出现了自相矛盾的兜底文案：\n%s", script)
	}
	if !strings.Contains(script, "SOURCE.txt") {
		t.Fatalf("脚本没有写下实际用的 ref：\n%s", script)
	}
	// SOURCE.txt 是一句关于这棵树的事实陈述：必须写在**建树成功之后**。
	// 写在前面的话，建树失败时会留下一句"<- main"的假话，而 agent 正是拿它
	// 判断"这是不是定稿契约"。
	if strings.Index(script, "SOURCE.txt") < strings.Index(script, "worktree add") {
		t.Fatalf("SOURCE.txt 写在建树之前：建树失败会留下假话\n%s", script)
	}
	// 兜底那次 fetch 必须被 set -e 管住（不再吞掉失败去用陈旧的 FETCH_HEAD）。
	if strings.Contains(script, "|| true") {
		t.Fatalf("兜底 fetch 被吞掉了失败：\n%s", script)
	}
}

// 集成 run 也要拿得到依赖仓：它的活是"跨仓库联调"，而此前 prompt 明说
// "你只能看到自己检出的那个仓库" —— 那等于让它靠记忆臆测对方仓库长什么样。
func TestBuildIntegrationCommandMaterializesDependencyRepositories(t *testing.T) {
	all := []string{"owner/api", "owner/client"}
	deps := []dependencyRepo{{FullName: "owner/client"}}
	command := buildIntegrationCommand("codex_cli", "MiniMax-M2", "owner/api", deps)
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("脚本内出现单引号：外层引号会提前闭合")
	}
	if !strings.Contains(inner, "for ENTRY in owner/client:; do") {
		t.Fatalf("集成脚本没有检出计划里的其它仓库：\n%s", inner)
	}
	// 集成 prompt 走的是**工作区里的 prompt.txt**（$(cat "$PROMPT")），不在命令串里 ——
	// 所以要断言 prompt 本体，而不是命令。断言错了对象会得出"没给路径"的假失败。
	prompt := integrationPrompt("cross_repo_regression", "owner/api", all, deps)
	if !strings.Contains(prompt, "../_deps/owner_client") {
		t.Fatalf("集成 prompt 没有给出依赖仓路径：\n%s", prompt)
	}
	if strings.Contains(prompt, "../_deps/owner_api") {
		t.Fatalf("集成 prompt 把**自己**也列成依赖仓了：\n%s", prompt)
	}
}
