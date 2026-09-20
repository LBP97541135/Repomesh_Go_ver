// Package roomnotice 把 RepoMesh 的真实事件投进 AgentTeams 的仓库团队房。
//
// web（建 issue）和 coordinator（规划派发/收产物）两个进程都要用它，所以放独立包。
//
// 三条不变的规矩（都在 coordinator 版用过一轮，别丢）：
//   - Notify 没有返回值：房间通知是观察面，投不出去不能让业务链路失败；
//   - Notify 不占用调用方时间：自带 goroutine + 20 秒上限，不会吃掉调用方的
//     超时预算；
//   - 消息用 RepoMesh 自己的服务身份发（凭据来自控制器 credentials/matrix-token，
//     按调用者签发），所以正文自报家门，不冒充某个 agent 在说话。
package roomnotice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
)

type Notifier struct {
	pool   *pgxpool.Pool
	matrix *agentteams.MatrixSession
}

// New 组装一个通知器。pool 或 matrix 为 nil 时 Notify 是安全空操作。
func New(pool *pgxpool.Pool, matrix *agentteams.MatrixSession) *Notifier {
	return &Notifier{pool: pool, matrix: matrix}
}

// NewFromEnv 按环境组装（两条凭据路见 agentteams.NewSessionFromEnv 的说明：
// 直配 MATRIX_ACCESS_TOKEN 优先，控制器换凭据兜底）。缺配置返回 nil —— Notify
// 对 nil 是空操作，业务行为与没有房间观察时完全一致。
func NewFromEnv(pool *pgxpool.Pool) *Notifier {
	controllerURL := strings.TrimRight(os.Getenv("AGENTTEAMS_CONTROLLER_URL"), "/")
	controller := &agentteams.Client{
		BaseURL: controllerURL,
		Token:   os.Getenv("AGENTTEAMS_CONTROLLER_TOKEN"),
	}
	if controllerURL == "" {
		controller = nil
	}
	session := agentteams.NewSessionFromEnv(controller)
	if session == nil {
		return nil
	}
	return New(pool, session)
}

// Notify 找到该 issue 的仓库团队房并投一条消息。投不出去只记一行。
//
// txnID 是幂等键：同一个逻辑事件重投时上游只认第一条，重试不刷屏。
// 并发上限：每条通知一个 goroutine，各自 20 秒封顶，积压随事件数有界。
func (n *Notifier) Notify(ctx context.Context, issueID, txnID, body string) {
	if n == nil || n.matrix == nil || n.pool == nil {
		return
	}
	// WithoutCancel 保留 ctx 的值、脱开它的取消：调用方的预算不该决定这条消息
	// 发不发得出去。上限由这里自己给。
	detached := context.WithoutCancel(ctx)
	go func() {
		sendCtx, cancel := context.WithTimeout(detached, 20*time.Second)
		defer cancel()
		n.deliver(sendCtx, issueID, txnID, body)
	}()
}

func (n *Notifier) deliver(ctx context.Context, issueID, txnID, body string) {
	roomID, err := n.roomForIssue(ctx, issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roomnotice: room lookup failed issue=%s: %v\n", issueID, err)
		return
	}
	if roomID == "" {
		// 这个 issue 的仓库还没有团队房（团队没建 / 房间号还没回读到）。不是错误。
		return
	}
	if _, err := n.matrix.SendMessage(ctx, roomID, txnID, body); err != nil {
		fmt.Fprintf(os.Stderr, "roomnotice: send failed issue=%s room=%s: %v\n", issueID, roomID, err)
	}
}

// roomForIssue 取这个 issue 范围里第一间真有房的仓库团队房。
//
// 0057 起 repository_teams 键为 (project_id, 扫描侧 repository_id),而 issue 范围里
// 存的是项目侧 repo_… id —— 必须先过 repositoryteams.RepoTeamResolutionQuery 把
// 两边对上,直接 join 永远命不中(两个 id 空间不同)。
//
// 定序仍按范围里的 repository_id:与 issues.GetIssueRooms 一致,两条读面给出不同的
// "第一间房"会让同一件事在两个地方显示成发生在不同房间。
func (n *Notifier) roomForIssue(ctx context.Context, issueID string) (string, error) {
	var roomID string
	err := n.pool.QueryRow(ctx, `
		SELECT t.team_room_id
		FROM repomesh_issues.issue_repository_scope s
		JOIN repomesh_projects.project_repositories pr
		  ON pr.project_id = s.project_id AND pr.repository_id = s.repository_id
		JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
		JOIN repomesh_projects.projects p ON p.id = pr.project_id
		JOIN repomesh_access.accounts a ON a.id = p.owner
		LEFT JOIN LATERAL (
		  SELECT scan.id FROM repomesh_scan.repositories scan
		  WHERE scan.organization_id = a.organization_id
		    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
		      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
		  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
		) sc ON true
		JOIN public.repository_teams t
		  ON t.project_id = s.project_id AND t.repository_id = sc.id
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
