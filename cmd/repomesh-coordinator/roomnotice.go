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
