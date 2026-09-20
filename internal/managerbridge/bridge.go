// Package managerbridge 双向桥（docs/design/manager-bridge.md §6-②）。
//
// 正向：人发进会话流的消息 → 投到该 issue 的队房，Matrix 确认才置 sent。
// ponytail: v1 以服务身份发言、正文带 [user: X] 前缀——RepoMesh 用户没有
// 已知的 Matrix 身份映射，masquerade 目标不存在；映射确认后再升级（设计 §8）。
package managerbridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// Enqueue 在人消息落库后登记一条待投递。失败只记一行——桥是观察面，
// 不能让人发消息这件事失败（设计 §4-①：会话流才是唯一真相）。
func (b *Bridge) Enqueue(ctx context.Context, issueID, conversationID, submissionID, actor, body string) {
	if b == nil || b.pool == nil || b.matrix == nil || body == "" || issueID == "" || conversationID == "" {
		return
	}
	_, err := b.pool.Exec(ctx,
		`INSERT INTO repomesh_messages.bridge_deliveries
		   (direction, issue_id, conversation_id, submission_id, actor, body)
		 VALUES ('human', $1, $2, NULLIF($3,''), $4, $5)
		 ON CONFLICT DO NOTHING`,
		issueID, conversationID, submissionID, actor, body)
	if err != nil {
		fmt.Printf("managerbridge: enqueue failed issue=%s: %v\n", issueID, err)
	}
}

// ForwardOnce 扫一小批 pending 的人消息投出去，返回处理条数。
// 由协调器按退避节奏调用；单条失败置回 pending 并记 last_error，
// 连续失败由调用方的退避兜住（不在桥里再造退避，A3/A4 的教训）。
func (b *Bridge) ForwardOnce(ctx context.Context) int {
	if b == nil || b.pool == nil || b.matrix == nil {
		return 0
	}
	rows, err := b.pool.Query(ctx,
		`SELECT id, issue_id, conversation_id, actor, body
		   FROM repomesh_messages.bridge_deliveries
		  WHERE direction='human' AND status='pending'
		  ORDER BY created_at LIMIT 20`)
	if err != nil {
		fmt.Printf("managerbridge: scan failed: %v\n", err)
		return 0
	}
	type item struct {
		id   int64
		issue, conv, actor, body string
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.issue, &it.conv, &it.actor, &it.body); err != nil {
			rows.Close()
			return len(batch)
		}
		batch = append(batch, it)
	}
	rows.Close()

	done := 0
	for _, it := range batch {
		roomID, err := roomnotice.RoomForIssue(ctx, b.pool, it.issue)
		if err != nil {
			b.mark(ctx, it.id, "pending", err)
			continue
		}
		if roomID == "" {
			// 没建队（物化前）：留 pending，桥启动补发（设计 §5）。不算失败。
			b.mark(ctx, it.id, "pending", nil)
			continue
		}
		txn := fmt.Sprintf("repomesh-bridge-%d", it.id)
		eventID, err := b.matrix.SendMessage(ctx, roomID, txn, messageBody(it.actor, it.body))
		if err != nil {
			b.mark(ctx, it.id, "pending", err)
			continue
		}
		if _, err := b.pool.Exec(ctx,
			`UPDATE repomesh_messages.bridge_deliveries
			    SET status='sent', matrix_event_id=$2, last_error=NULL, updated_at=now()
			  WHERE id=$1`, it.id, eventID); err != nil {
			fmt.Printf("managerbridge: mark sent failed id=%d: %v\n", it.id, err)
		}
		done++
	}
	return done
}

func (b *Bridge) mark(ctx context.Context, id int64, status string, cause error) {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, _ = b.pool.Exec(ctx,
		`UPDATE repomesh_messages.bridge_deliveries
		    SET status=$2, last_error=NULLIF($3,''), attempts=attempts+1, updated_at=now()
		  WHERE id=$1`, id, status, msg)
}

// messageBody 服务身份发言，正文自报家门（v1，见文件头 ponytail 注）。
func messageBody(actor, body string) string {
	if actor == "" {
		return body
	}
	return "[user: " + actor + "] " + body
}

// Compile-time pins.
var _ = errors.Is
var _ = pgx.ErrNoRows
var _ = time.Second
