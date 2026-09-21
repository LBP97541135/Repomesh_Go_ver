// Package managerbridge 双向桥（docs/design/manager-bridge.md §6-②）。
//
// 正向：人发进会话流的消息 → 投到该 issue 的队房，Matrix 确认才置 sent。
// ponytail: v1 以服务身份发言、正文带 [user: X] 前缀——RepoMesh 用户没有
// 已知的 Matrix 身份映射，masquerade 目标不存在；映射确认后再升级（设计 §8）。
package managerbridge

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/roomnotice"
)

type Bridge struct {
	pool   *pgxpool.Pool
	matrix *agentteams.MatrixSession
}

// New 组装。任一为 nil 时所有方法都是安全空操作（与 roomnotice 同一契约）。
func New(pool *pgxpool.Pool, matrix *agentteams.MatrixSession) *Bridge {
	return &Bridge{pool: pool, matrix: matrix}
}

// NewFromEnv 按环境组装（与 roomnotice.NewFromEnv 同一条凭据路，见其说明）。
// 缺配置返回 nil：桥对 nil 是空操作，行为与没有桥时完全一致。
func NewFromEnv(pool *pgxpool.Pool) *Bridge {
	controllerURL := strings.TrimRight(os.Getenv("AGENTTEAMS_CONTROLLER_URL"), "/")
	var controller *agentteams.Client
	if controllerURL != "" {
		controller = &agentteams.Client{BaseURL: controllerURL, Token: os.Getenv("AGENTTEAMS_CONTROLLER_TOKEN")}
	}
	session := agentteams.NewSessionFromEnv(controller)
	if session == nil {
		return nil
	}
	return New(pool, session)
}

// ForwardOnce 扫一小批**未投递的人消息**投进该 issue 的队房，返回处理条数。
//
// 不设 Enqueue 推送口：会话流里的 conversation_messages 行本身就是"人说了话"这个
// 事实（设计 §4-①），桥只按主键差集找没投过的，投成功才落 bridge_deliveries。
// 少一条写路径、少一处依赖注入，也不存在"两个写不原子"——那个坑就是被这么消掉的。
func (b *Bridge) ForwardOnce(ctx context.Context) int {
	if b == nil || b.pool == nil || b.matrix == nil {
		return 0
	}
	rows, err := b.pool.Query(ctx, `
		SELECT m.id, m.conversation_id, i.id, COALESCE(a.display_name, m.actor_id, ''), m.body
		  FROM repomesh_messages.conversation_messages m
		  JOIN repomesh_issues.issues i ON i.main_conversation_id = m.conversation_id
		  LEFT JOIN repomesh_access.accounts a ON a.id = m.actor_id
		  LEFT JOIN repomesh_messages.bridge_deliveries d
		         ON d.submission_id = m.id AND d.direction = 'human'
		 WHERE m.author_kind = 'user' AND d.id IS NULL
		 ORDER BY m.id LIMIT 20`)
	if err != nil {
		fmt.Printf("managerbridge: scan failed: %v\n", err)
		return 0
	}
	type item struct{ msgID, conv, issue, actor, body string }
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.msgID, &it.conv, &it.issue, &it.actor, &it.body); err != nil {
			rows.Close()
			return len(batch)
		}
		batch = append(batch, it)
	}
	rows.Close()

	sent := 0
	for _, it := range batch {
		roomID, err := roomnotice.RoomForIssue(ctx, b.pool, it.issue)
		if err != nil || roomID == "" {
			// 取房失败或还没建队（物化前）：**不落台账**，下一拍原样重来
			// ——补发语义就是"没投出去的下次还找得到"（设计 §5）。
			continue
		}
		txn := "repomesh-bridge-" + it.msgID
		eventID, err := b.matrix.SendMessage(ctx, roomID, txn, messageBody(it.actor, it.body))
		if err != nil {
			fmt.Printf("managerbridge: send failed issue=%s room=%s: %v\n", it.issue, roomID, err)
			continue
		}
		if _, err := b.pool.Exec(ctx,
			`INSERT INTO repomesh_messages.bridge_deliveries
			   (direction, issue_id, conversation_id, submission_id, matrix_event_id, actor, body, status)
			 VALUES ('human', $1, $2, $3, $4, $5, $6, 'sent')
			 ON CONFLICT DO NOTHING`,
			it.issue, it.conv, it.msgID, eventID, it.actor, it.body); err != nil {
			fmt.Printf("managerbridge: ledger write failed msg=%s: %v\n", it.msgID, err)
		}
		sent++
	}
	return sent
}

// ReverseOnce 把队房里 Manager 的话搬回会话流（设计 §9-③）：
//   - 不需要按 sender 过滤桥自己：正向发出去的 event_id 都在台账里，按 event_id
//     去重时自己的话天然被滤掉；
//   - **首次见一个房间只记基线**（status='baseline'，不写会话流），否则第一次轮询
//     会把房间历史（规划通知、系统消息）一股脑灌进聊天室；
//   - 房间→issue 复用台账里"人先说过的那些 issue"。
//     ponytail: 天花板 = Manager 主动先说话的场景桥不到（没有人的消息就没有台账行）。
func (b *Bridge) ReverseOnce(ctx context.Context) int {
	if b == nil || b.pool == nil || b.matrix == nil {
		return 0
	}
	rooms, err := b.pool.Query(ctx, `
		SELECT DISTINCT issue_id, conversation_id
		  FROM repomesh_messages.bridge_deliveries WHERE direction='human' LIMIT 50`)
	if err != nil {
		return 0
	}
	type target struct{ issue, conv string }
	var targets []target
	for rooms.Next() {
		var tg target
		if err := rooms.Scan(&tg.issue, &tg.conv); err == nil {
			targets = append(targets, tg)
		}
	}
	rooms.Close()

	bridged := 0
	for _, tg := range targets {
		var roomID string
		if roomID, err = roomnotice.RoomForIssue(ctx, b.pool, tg.issue); err != nil || roomID == "" {
			continue
		}
		messages, err := b.matrix.RoomMessages(ctx, roomID, 20)
		if err != nil {
			continue
		}
		var baselined bool
		_ = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_messages.bridge_deliveries
			WHERE issue_id=$1 AND direction='manager')`, tg.issue).Scan(&baselined)
		for _, m := range messages {
			if m.Body == "" {
				continue
			}
			status, body := "received", m.Body
			if !baselined {
				status, body = "baseline", ""
			}
			tag, err := b.pool.Exec(ctx, `
				INSERT INTO repomesh_messages.bridge_deliveries
				  (direction, issue_id, conversation_id, matrix_event_id, actor, body, status)
				VALUES ('manager', $1, $2, $3, $4, $5, $6)
				ON CONFLICT (matrix_event_id) DO NOTHING`,
				tg.issue, tg.conv, m.EventID, m.Sender, body, status)
			if err != nil || tag.RowsAffected() == 0 || status != "received" {
				continue
			}
			if _, err := b.pool.Exec(ctx, `
				INSERT INTO repomesh_messages.conversation_messages
				  (id, project_id, conversation_id, sequence, author_kind, actor_id, body)
				SELECT 'msg_'||replace(gen_random_uuid()::text,'-',''), i.project_id, $1,
				       COALESCE((SELECT MAX(sequence) FROM repomesh_messages.conversation_messages
				                  WHERE project_id=i.project_id AND conversation_id=$1),0)+1,
				       'service', 'manager', $2
				  FROM repomesh_issues.issues i WHERE i.id=$3`,
				tg.conv, m.Body, tg.issue); err != nil {
				fmt.Printf("managerbridge: reverse insert failed issue=%s: %v\n", tg.issue, err)
				continue
			}
			bridged++
		}
	}
	return bridged
}

// messageBody 服务身份发言，正文自报家门（v1，见文件头 ponytail 注）。
func messageBody(actor, body string) string {
	if actor == "" {
		return body
	}
	return "[user: " + actor + "] " + body
}
