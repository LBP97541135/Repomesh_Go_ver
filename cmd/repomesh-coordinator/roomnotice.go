package main

import (
	"fmt"
	"strings"

	"repomesh.local/repomesh/internal/roomnotice"
)

// 规划链路的通知文案。投递机制（查房、凭据、幂等、不占预算）在 internal/roomnotice，
// 这里只管"这一步说什么"。

func planningDispatchedNotice(step int, role string) string {
	return fmt.Sprintf("【RepoMesh】规划第 %d 步已派给处理员（角色 %s），正在分析…", step, role)
}

func planningCompletedNotice(step int, role string) string {
	return fmt.Sprintf("【RepoMesh】规划第 %d 步完成（角色 %s）。", step, role)
}

// planningFailedNotice 带原因。原因里可能有 agent 的 stderr 尾巴，截断：
// 房间里一条消息不该变成一面墙。判据是"不随输入增长"，不是某个具体字数。
func planningFailedNotice(step int, reason string) string {
	return fmt.Sprintf("【RepoMesh】规划第 %d 步失败：%s", step, truncateRunes(reason, 400))
}

// gateTimeoutNotice 门超时代选的房间文案(spec 2026-09-20 §3.4;幂等键由调用方
// 给 gate:{issue}:timeout)。代选是系统替人做的决定,房间里必须留一条记录。
func gateTimeoutNotice(repositoryCount int) string {
	return fmt.Sprintf("【RepoMesh】选仓门 10 分钟未确认,已按 AI 建议代选 %d 个仓库(可回 issue 页调整范围)。", repositoryCount)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// 编译期钉住：coordinator 用的是共享通知器，别在本地再长一份。
var _ = roomnotice.New
var _ = strings.TrimSpace

// gateOpenedNotice 选仓门开启。只在候选步成功后发一次。
func gateOpenedNotice(suggested int) string {
	return "【RepoMesh】选仓门已开：AI 建议 " + itoa(suggested) + " 个仓库，等人勾选或让 AI 定。"
}

// gateAuditNotice 查漏结果(只提示,不擅自改范围)。
func gateAuditNotice(missing []string) string {
	joined := missing[0]
	if len(missing) > 1 {
		joined = joined + " 等 " + itoa(len(missing)) + " 个"
	}
	return "【RepoMesh】查漏：已确认范围可能漏了 " + joined + "，请确认补不补。"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
