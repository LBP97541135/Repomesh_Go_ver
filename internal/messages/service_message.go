package messages

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RecordServiceMessage 由**平台自己**往协作房间写一条服务消息。
//
// 2026-09-20：右栏聊天窗口此前只有真人发言这一条写入口，而 issue 的协作房间里
// 从来没有人说过话 —— 加上 tasks.conversation_id 根本没人填，界面只能永远显示
// 「消息流加载中…」。房间要真的有用，就得把流水线里**真实发生过的事**记进去：
// 派了谁、跑成什么样、测试结论、经理批没批。
//
// 走的是真人发言同一张表、同一套序号分配规则（在会话行锁下取 MAX+1），所以右栏
// 看到的时序与真人发言完全一致，不需要前端另开一套渲染。
//
// 房间不存在就什么都不写（fail-closed）：宁可这一条不出现，也不往一个不存在的
// 会话里塞行（外键会直接拒绝，但显式判断能给出更清楚的原因）。
func (s *MessageService) RecordServiceMessage(ctx context.Context, projectID, conversationID, actorID, body string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	return RecordServiceMessageWithPool(ctx, s.pool, projectID, conversationID, actorID, body)
}

// RecordServiceMessageWithPool 是**只拿得到一个连接池**的调用方（host-executor、
// coordinator 这些没有 MessageService 的进程）用的入口：实现只有一份，避免两个
// 进程各自抄一遍序号分配规则、抄出两种时序。
func RecordServiceMessageWithPool(ctx context.Context, pool *pgxpool.Pool, projectID, conversationID, actorID, body string) error {
	if pool == nil {
		return nil
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(body) == "" {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// 锁住会话行再取序号：与真人发言共用同一条排序规则（设计 §7 分页规则）。
	var live string
	if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_issues.conversations
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL FOR UPDATE`,
		projectID, conversationID).Scan(&live); err != nil {
		return nil
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1
		FROM repomesh_messages.conversation_messages
		WHERE project_id=$1 AND conversation_id=$2`, projectID, conversationID).Scan(&sequence); err != nil {
		return err
	}
	messageID, err := newID("msg_")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.conversation_messages
		(id, project_id, conversation_id, sequence, author_kind, actor_id, body)
		VALUES ($1,$2,$3,$4,'service',$5,$6)`,
		messageID, projectID, conversationID, sequence, actorID, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
