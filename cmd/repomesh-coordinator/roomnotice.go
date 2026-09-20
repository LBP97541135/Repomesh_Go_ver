package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
)

// roomNotifier 把规划链路上的**真实事件**投进 AgentTeams 的仓库团队房。
//
// 为什么放在 coordinator 而不是 discovery 服务里：往房间投消息是**外部副作用**，
// 域代码不该依赖它（AGENTS.md：域不依赖基础设施适配器）。这里同时是组合根。
//
// 为什么 Notify 没有返回值：房间通知是**观察面**，不是链路的必经环节。它投不出去
// 不该让规划失败——否则"看得见"会变成"跑不动"。用签名而不是靠调用方自觉，
// 是因为前者拦得住将来的人，后者只在今天拦得住。
//
// 归属说明（不装）：RepoMesh 用**自己的**服务身份发这些消息（凭据来自控制器的
// credentials/matrix-token，按调用者签发），所以房间里这条的发送者是 RepoMesh，
// 不是 Manager 那个 Matrix 身份。消息因此只说"发生了什么"，不冒充某个 agent 在说话。
type roomNotifier struct {
	pool   *pgxpool.Pool
	matrix *agentteams.MatrixSession
}

// newRoomNotifier 按环境组装。缺任一环境变量就返回 nil —— Notify 对 nil 是安全的
// 空操作，整条链路行为与没有房间观察时完全一致（这也是它敢装在 coordinator 里的原因）。
func newRoomNotifier(pool *pgxpool.Pool) *roomNotifier {
	controllerURL := strings.TrimRight(os.Getenv("AGENTTEAMS_CONTROLLER_URL"), "/")
	homeserver := strings.TrimRight(os.Getenv("MATRIX_HOMESERVER_URL"), "/")
	if controllerURL == "" || homeserver == "" {
		return nil
	}
	return &roomNotifier{
		pool: pool,
		matrix: &agentteams.MatrixSession{
			Controller: &agentteams.Client{
				BaseURL: controllerURL,
				Token:   os.Getenv("AGENTTEAMS_CONTROLLER_TOKEN"),
			},
			Homeserver: homeserver,
		},
	}
}

// planningDispatchedNotice 是"派下去、正在干活"那一条。
func planningDispatchedNotice(step int, role string) string {
	return fmt.Sprintf("【RepoMesh】规划第 %d 步已派给处理员（角色 %s），正在分析…", step, role)
}

// planningCompletedNotice 是"这一步成了"那一条。
func planningCompletedNotice(step int, role string) string {
	return fmt.Sprintf("【RepoMesh】规划第 %d 步完成（角色 %s）。", step, role)
}

// planningFailedNotice 是"这一步没成"那一条，带上原因。
//
// 原因里可能带 agent 的 stderr 尾巴，所以截断：房间里一条消息不该变成一面墙。
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

// Notify 找到该 issue 的仓库团队房并投一条消息。投不出去只记一行日志。
//
// txnID 是幂等键：同一个逻辑事件重投时上游只认第一条，所以协调器重试不会刷屏。
func (n *roomNotifier) Notify(ctx context.Context, issueID, txnID, body string) {
	if n == nil || n.matrix == nil || n.pool == nil {
		return
	}
	roomID, err := n.roomForIssue(ctx, issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coordinator: room lookup failed issue=%s: %v\n", issueID, err)
		return
	}
	if roomID == "" {
		// 这个 issue 的仓库还没有团队房（团队没建 / 房间号还没回读到）。不是错误。
		return
	}
	if _, err := n.matrix.SendMessage(ctx, roomID, txnID, body); err != nil {
		fmt.Fprintf(os.Stderr, "coordinator: room notice failed issue=%s room=%s: %v\n", issueID, roomID, err)
	}
}

// roomForIssue 取这个 issue 范围里第一间真有房的仓库团队房。
//
// 定序必须与 issues.GetIssueRooms 一致（按 repository_id）：两条读面给出不同的
// "第一间房"会让同一件事在两个地方显示成发生在不同房间。
func (n *roomNotifier) roomForIssue(ctx context.Context, issueID string) (string, error) {
	var roomID string
	err := n.pool.QueryRow(ctx, `
		SELECT t.team_room_id
		FROM repomesh_issues.issue_repository_scope s
		JOIN public.repository_teams t ON t.repository_id = s.repository_id
		WHERE s.issue_id = $1 AND COALESCE(NULLIF(t.team_room_id, ''), '') <> ''
		ORDER BY s.repository_id
		LIMIT 1`, issueID).Scan(&roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return roomID, nil
}
