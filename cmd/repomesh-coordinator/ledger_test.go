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
		"把运费改成满 900 免运费", "owner/name", "att_1", "标题", "iss_1", "技能原文")
	if err != nil {
		t.Fatal(err)
	}
	testCmd, err := buildTestCommand("codex_cli", "MiniMax-M2", "add a file", "owner/name", "att_1", "title", "")
	if err != nil {
		t.Fatal(err)
	}
	integration := buildIntegrationCommand("codex_cli", "MiniMax-M2", "owner/name")
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
		"把运费改成满 900 免运费", "owner/name", "att_1", "满900免运费", "iss_1", "")
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
		"do the thing", "owner/name", "att_1", "title", "iss_1", skillDoc)
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
	command := buildIntegrationCommand("codex_cli", "MiniMax-M2", "owner/name")
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
