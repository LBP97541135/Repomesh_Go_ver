package main

import (
	"strings"
	"testing"
)

// 交付脚本的两条硬约束（都在线上踩过）：
//  1. 整段脚本被 bash -c '...' 包着 —— 脚本内**不能出现单引号**，否则外层引号提前
//     闭合，交付序列会在 agent 之后被静默截断（commit/push/开 PR 全没了）；
//  2. 每个 task 必须走**一棵新的 worktree**（2026-09-20 用户裁定），而不是每次整仓 clone。
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
		"worktree add --detach --force \"$BASE/repo\" FETCH_HEAD",
		"_bases/$SLUG",
		"rm -rf \"$BASE/repo\"",
		"git push origin HEAD:refs/heads/$B",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("脚本缺少 %q：%s", want, inner)
		}
	}
	if strings.Contains(inner, "git clone --depth 5 https://x-access-token:$T@github.com/$R.git repo\n") {
		t.Fatal("还在用整仓 clone 建工作区（应改为共享基础克隆 + worktree add）")
	}
	if !strings.Contains(inner, "cd \"$BASE/repo\"") {
		t.Fatalf("必须进入基础仓库中的 worktree：%s", inner)
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
	if !strings.Contains(inner, "worktree add --detach --force \"$BASE/repo\" FETCH_HEAD") {
		t.Fatalf("集成脚本没有走 worktree：%s", inner)
	}
	if !strings.Contains(inner, "cd \"$BASE/repo\"") || !strings.Contains(inner, `$(cat "$PROMPT")`) {
		t.Fatalf("集成脚本必须进入 worktree 并读取任务工作区的 prompt：%s", inner)
	}
}

func TestBuildTestCommandUsesSharedWorktree(t *testing.T) {
	command, err := buildTestCommand("codex_cli", "MiniMax-M2", "add a file", "owner/name", "att_1", "title", "")
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	for _, want := range []string{
		"_bases/$SLUG",
		`cd "$BASE/repo"`,
		"git status --short",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("测试脚本缺少 %q：%s", want, inner)
		}
	}
	if strings.Contains(inner, "\ncd repo\n") {
		t.Fatalf("测试脚本仍使用旧工作区路径：%s", inner)
	}
}
