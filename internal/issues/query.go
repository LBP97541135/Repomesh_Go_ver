package issues

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
)

// IssueListItem is one row of the project issue list (browser contract §8):
// no body text, no execution status, fixed ID ordering.
type IssueListItem struct {
	ID              string      `json:"id"`
	Number          int64       `json:"number"`
	Title           string      `json:"title"`
	RepositoryIDs   []string    `json:"repositoryIds"`
	MainChangeSetID string      `json:"mainChangeSetId"`
	Source          IssueSource `json:"source"`
	CreatedAt       time.Time   `json:"createdAt"`
	Revision        string      `json:"revision"`
	// —— 2026-09-19 方案 A：控制台列表需要的派生读模型 ——
	// 前端是对着旧 Python 读模型写的（issue_id/phase/round_count/…）。这些字段
	// 由本文件的 SQL 从 rounds 的替代物与发现链派生，**不是编造**：每个字段的
	// 语义来源见下面的注释。映射关系：
	//   round_count  ← 派工代际数（Go 无 rounds 表；代际 = 一次派工波次）
	//   team_count   ← 仓库数（团队按 project×repository 建，一仓一队）
	State           string `json:"state"` // open | closed（archived_at）
	Phase           string `json:"phase"` // 八相：发现链状态的显式映射
	PhaseNote       string `json:"phaseNote"`
	PlanVersion     string `json:"planVersion"`     // plans.plan_version（v1…），空=还没计划
	RoundCount      int    `json:"roundCount"`      // count(DISTINCT attempts.reservation_generation)
	TeamCount       int    `json:"teamCount"`       // = RepositoryCount（一仓一队）
	RepositoryCount int    `json:"repositoryCount"` // issue_repository_scope 计数
	PendingPlanning bool   `json:"pendingPlanning"` // 需求已落库但还没物化
	RequirementText string `json:"requirementText"`
	IssueKey        string `json:"issueKey"`
	OrganizationID  string `json:"organizationId"`
}

// IssueSource carries the origin reference; conversationId is an identity
// reference, never a read grant.
type IssueSource struct {
	Kind           string `json:"kind"`
	ConversationID string `json:"conversationId"`
}

// IssuePage is the paginated issue list.
type IssuePage struct {
	Items       []IssueListItem `json:"items"`
	NextCursor  *string         `json:"nextCursor"`
	OpenCount   int             `json:"openCount"`
	ClosedCount int             `json:"closedCount"`
}

// IssueListQuery carries the §8 unified pagination and filters.
type IssueListQuery struct {
	Text         string
	RepositoryID string
	// State 是 issue 列表两个标签页的过滤：open / closed / all。
	//
	// 2026-09-21：此前这个字段**根本不存在**，handler 也没读 `state` 查询参数 ——
	// 控制台点「Open / Closed」传了值，服务端直接丢掉，两个标签页永远返回同一份
	// 全量列表（用户报的"open 和 close 页没有过滤，都是全量展示"）。
	// 口径与逐行 state 同源：归档即 closed（`archived_at`）。
	// 空值收敛成 all（= 修复前的行为），所以老脚本/老调用方不会静默少行。
	State  string
	Cursor       string
	Limit        int
}

// ParseIssueListQuery applies the documented limits: limit 1..100 default 50,
// q at most 200 Unicode scalars.
func ParseIssueListQuery(text, repositoryID, state, cursor string, limit int) (IssueListQuery, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return IssueListQuery{}, failure(422, "VALIDATION_FAILED")
	}
	if len([]rune(text)) > 200 {
		return IssueListQuery{}, failure(422, "VALIDATION_FAILED")
	}
	// 认这三个值 + 空；别的一律 422，而不是悄悄当成 all —— 写错一个字母就拿到
	// "看起来对"的全量列表，那正是这个 bug 能藏这么久的原因。
	switch state {
	case "":
		state = "all"
	case "open", "closed", "all":
	default:
		return IssueListQuery{}, failure(422, "VALIDATION_FAILED")
	}
	return IssueListQuery{Text: text, RepositoryID: repositoryID, State: state, Cursor: cursor, Limit: limit}, nil
}

// ListIssues returns the readable issue list for one project. Only issues whose
// full content scope is readable are returned; an unknown authorization check
// during candidate evaluation collapses to 503 rather than a silent filter.
func (s *Service) ListIssues(ctx context.Context, principal access.ProjectPrincipal, projectID string, query IssueListQuery) (IssuePage, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return IssuePage{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return IssuePage{}, err
	}
	if _, err := readProjectRow(ctx, tx, projectID, principal.ActorID()); err != nil {
		return IssuePage{}, err
	}
	scope := queryCursorScope{
		actor:     principal.ActorID(),
		kind:      "issues",
		projectID: projectID,
		query:     cursorScopeFingerprint(principal.ActorID(), "issues", projectID, query.Text, query.RepositoryID, query.State, query.Limit),
		limit:     query.Limit,
	}
	after := ""
	if query.Cursor != "" {
		cursor, readErr := readIssueCursor(ctx, tx, query.Cursor, scope)
		if readErr != nil {
			return IssuePage{}, readErr
		}
		after = cursor.afterID
	}
	where := `WHERE i.project_id=$1 AND i.removed_at IS NULL AND i.id>$2`
	args := []any{projectID, after}
	// 占位符改成动态序号。此前是写死的 $3 / $4：只要"只按仓库过滤、不传 q"，
	// 参数就只有 3 个却引用了 $4，PostgreSQL 直接报参数不存在 → 503。
	next := func(value any) string {
		args = append(args, value)
		return "$" + itoa(len(args))
	}
	if query.Text != "" {
		where += ` AND strpos(lower(i.title),lower(` + next(query.Text) + `))>0`
	}
	if query.RepositoryID != "" {
		where += ` AND EXISTS (SELECT 1 FROM repomesh_issues.issue_repository_scope s
			WHERE s.issue_id=i.id AND s.repository_id=` + next(query.RepositoryID) + `)`
	}
	// state：与逐行 state 同源（归档即 closed），两个标签页各看自己那一半。
	// 用 archived_at 而不是别处派生 —— 那正是 SELECT 里 CASE 的依据，两处必须同源。
	switch query.State {
	case "open":
		where += ` AND i.archived_at IS NULL`
	case "closed":
		where += ` AND i.archived_at IS NOT NULL`
	}
	args = append(args, query.Limit+1)
	rows, err := tx.Query(ctx, `SELECT i.id,i.number,i.title,i.main_changeset_id,i.revision,i.created_at,i.main_conversation_id,
		       CASE WHEN i.archived_at IS NULL THEN 'open' ELSE 'closed' END,
		       COALESCE(i.description,''),
		       COALESCE(pj.organization_id::text,''),
		       (SELECT count(*) FROM repomesh_issues.issue_repository_scope s WHERE s.issue_id=i.id),
		       (SELECT count(DISTINCT a.reservation_generation) FROM repomesh_execution.attempts a WHERE a.issue_id=i.id),
		       COALESCE((SELECT p.plan_version FROM public.plans p
		                   JOIN public.tasks t ON t.plan_id=p.id
		                  WHERE t.source_ref->>'issueId'=i.id
		                  ORDER BY p.plan_version DESC LIMIT 1),''),
		       COALESCE(d.materialization IS NULL OR d.materialization='null'::jsonb, true),
		       CASE
		         WHEN d.issue_id IS NULL THEN 'contract'
		         WHEN d.materialization IS NOT NULL AND d.materialization <> 'null'::jsonb THEN 'execute'
		         WHEN d.plan IS NOT NULL AND d.plan <> 'null'::jsonb THEN 'plan'
		         WHEN d.approval->>'state' = 'approved' THEN 'plan'
		         WHEN d.classification IS NOT NULL AND d.classification <> 'null'::jsonb THEN 'validate'
		         ELSE 'contract'
		       END
		FROM repomesh_issues.issues i
		LEFT JOIN repomesh_issues.issue_discoveries d ON d.issue_id = i.id
		LEFT JOIN repomesh_projects.projects pj ON pj.id = i.project_id
		`+where+` ORDER BY i.id LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return IssuePage{}, unavailable()
	}
	defer rows.Close()
	items := []IssueListItem{}
	type rowMeta struct {
		revision       string
		createdAt      time.Time
		conversationID string
	}
	scan := map[string]rowMeta{}
	for rows.Next() {
		var item IssueListItem
		var meta rowMeta
		var conversationID string
		if rows.Scan(&item.ID, &item.Number, &item.Title, &item.MainChangeSetID, &meta.revision, &meta.createdAt, &conversationID,
			&item.State, &item.RequirementText, &item.OrganizationID,
			&item.RepositoryCount, &item.RoundCount, &item.PlanVersion,
			&item.PendingPlanning, &item.Phase) != nil {
			return IssuePage{}, unavailable()
		}
		// team_count 与 repository_count 同源：团队按 project×repository 建（一仓一队），
		// 所以"会承接该 issue 的团队数"就是"该 issue 覆盖的仓库数"。
		item.TeamCount = item.RepositoryCount
		item.IssueKey = "#" + strconv.FormatInt(item.Number, 10)
		item.RepositoryIDs = []string{}
		item.Source = IssueSource{Kind: "issue_page", ConversationID: conversationID}
		items = append(items, item)
		scan[item.ID] = rowMeta{revision: meta.revision, createdAt: meta.createdAt, conversationID: conversationID}
	}
	if rows.Err() != nil {
		return IssuePage{}, unavailable()
	}
	result := IssuePage{}
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	for index := range items {
		meta := scan[items[index].ID]
		items[index].Revision = meta.revision
		items[index].CreatedAt = meta.createdAt
		repositories, err := issueRepositoryIDs(ctx, tx, items[index].ID)
		if err != nil {
			return IssuePage{}, err
		}
		items[index].RepositoryIDs = repositories
	}
	result.Items = items
	// 信封计数（2026-09-19 方案 A）：控制台侧栏徽标读 open_count。口径与逐行 state
	// 一致——归档即 closed，已删除的不计。
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE archived_at IS NULL),
		       count(*) FILTER (WHERE archived_at IS NOT NULL)
		FROM repomesh_issues.issues WHERE project_id=$1 AND removed_at IS NULL`, projectID).
		Scan(&result.OpenCount, &result.ClosedCount); err != nil {
		return IssuePage{}, unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return IssuePage{}, unavailable()
	}
	if len(items) == query.Limit && len(items) > 0 {
		cursor := queryCursorRecord{id: newCursorID(), scope: scope, afterID: items[len(items)-1].ID, expiresAt: time.Now().Add(10 * time.Minute)}
		writeTx, err := s.beginCreate(ctx)
		if err != nil {
			return IssuePage{}, err
		}
		if err := writeIssueCursor(ctx, writeTx, cursor); err != nil {
			rollbackTx(writeTx)
			return IssuePage{}, err
		}
		if err := writeTx.Commit(ctx); err != nil {
			return IssuePage{}, unavailable()
		}
		result.NextCursor = &cursor.id
	}
	return result, nil
}

// readProjectRow 校验"这个项目存在**且属于调用者**"。
//
// 2026-09-19 账号隔离：此前只查存在性 —— 任何登录账号只要知道别人的 project id
// 就能读到那个项目的 issue 列表 / issue 详情 / rooms。公有部署（一账号一空间）
// 下这是硬伤，所以归属校验落在这一处，三个调用点共用。
func readProjectRow(ctx context.Context, tx pgx.Tx, projectID, actor string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_projects.projects WHERE id=$1 AND owner=$2 AND removed_at IS NULL)`, projectID, actor).Scan(&exists)
	if err != nil {
		return false, unavailable()
	}
	if !exists {
		return false, failure(404, "RESOURCE_NOT_FOUND")
	}
	return true, nil
}

func issueRepositoryIDs(ctx context.Context, tx pgx.Tx, issueID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT repository_id FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1 ORDER BY repository_id`, issueID)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil, unavailable()
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

type queryCursorScope struct {
	actor     string
	kind      string
	projectID string
	query     string
	limit     int
}

// cursorScopeFingerprint 把「这条游标属于谁、在哪个项目、查什么、每页多大」压成
// 一个可以直接入库的指纹。
//
// 2026-09-20 线上实测（全库日志 65 次，与 503 一一对应）：原先直接把
// `query.Text + "\x00" + query.RepositoryID` 存进 repomesh_projects.cursors.query。
// Postgres 的 text 类型**不接受 NUL 字节**，于是那条 INSERT 每次都以
// `invalid byte sequence for encoding "UTF8": 0x00`（portal parameter $5）失败，
// writeIssueCursor 把它判成 503 RESULT_UNCONFIRMED —— 界面上就是"服务端暂时不可用"，
// 而且只在分页写游标时出现，所以看起来时有时无。
//
// 换成 sha256 十六进制：不含 NUL，也不把用户输入原样落库；分隔符用 \x1f（US）
// 而不是 \x00，同样只是为了拼接不产生歧义，它不会进数据库。
// 指纹必须把 state 算进去：否则「open 标签页第一页」的游标能在「closed 标签页」
// 上重放，翻页会翻到另一个集合里去（而且不报错，静默错行）。
func cursorScopeFingerprint(actor, kind, projectID, text, repositoryID, state string, limit int) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{actor, kind, projectID, text, repositoryID, state, strconv.Itoa(limit)}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

type queryCursorRecord struct {
	id        string
	scope     queryCursorScope
	afterID   string
	expiresAt time.Time
}

// readIssueCursor validates one stored cursor against the query scope.
func readIssueCursor(ctx context.Context, tx pgx.Tx, id string, scope queryCursorScope) (queryCursorRecord, error) {
	var result queryCursorRecord
	var actor, kind, projectID, query string
	var limit int
	err := tx.QueryRow(ctx, `SELECT actor,kind,project_id,query,page_limit,after_id,expires_at
		FROM repomesh_projects.cursors WHERE id=$1`, id).Scan(&actor, &kind, &projectID, &query, &limit, &result.afterID, &result.expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return queryCursorRecord{}, failure(400, "INVALID_CURSOR")
		}
		return queryCursorRecord{}, unavailableWith(err)
	}
	if actor != scope.actor || kind != scope.kind || projectID != scope.projectID || query != scope.query || limit != scope.limit {
		return queryCursorRecord{}, failure(400, "INVALID_CURSOR")
	}
	if !result.expiresAt.After(time.Now()) {
		return queryCursorRecord{}, failure(409, "CURSOR_EXPIRED")
	}
	result.id = id
	result.scope = scope
	return result, nil
}

func writeIssueCursor(ctx context.Context, tx pgx.Tx, record queryCursorRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.cursors(id,actor,kind,project_id,query,page_limit,after_id,project_revision,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'',$8)`,
		record.id, record.scope.actor, record.scope.kind, record.scope.projectID, record.scope.query, record.scope.limit, record.afterID, record.expiresAt)
	if err != nil {
		return unavailableWith(err)
	}
	return nil
}

func newCursorID() string {
	id, err := newID("cur_")
	if err != nil {
		return ""
	}
	return id
}

// IssueDetail is the minimal post-creation snapshot (creation contract §7).
type IssueDetail struct {
	ID                 string      `json:"id"`
	Number             int64       `json:"number"`
	ProjectID          string      `json:"projectId"`
	Revision           string      `json:"revision"`
	Title              string      `json:"title"`
	Description        string      `json:"description"`
	RepositoryIDs      []string    `json:"repositoryIds"`
	AcceptanceCriteria []string    `json:"acceptanceCriteria"`
	MainChangeSetID    string      `json:"mainChangeSetId"`
	Source             IssueSource `json:"source"`
	CreatedAt          time.Time   `json:"createdAt"`
	// HitlMode 是这次 issue 的人审门模式：ai = 自动托管（处理员代行 ③ 分档审批与
	// ⑤ 物化确认），hitl = 门等真人。它是服务端事实（0053），工作台按它渲染徽标与
	// 门的行为 —— 此前只活在浏览器 sessionStorage 里，刷新/换浏览器就丢。
	HitlMode string `json:"hitlMode"`
	// MergeMode 是这次 issue 的**合并方式**：auto = 交付闸门一开就自动把 PR 合掉，
	// manual = 等人在交付序列上逐条点合并（缺省）。见迁移 0064 与 mergeModeOrDefault。
	MergeMode string `json:"mergeMode"`
}

// GetIssue returns the issue detail snapshot after re-checking read
// authorization on the issue's content scope.
func (s *Service) GetIssue(ctx context.Context, principal access.ProjectPrincipal, projectID, issueID string) (IssueDetail, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return IssueDetail{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return IssueDetail{}, err
	}
	if _, err := readProjectRow(ctx, tx, projectID, principal.ActorID()); err != nil {
		return IssueDetail{}, err
	}
	var detail IssueDetail
	var criteria []byte
	var conversationID string
	err = tx.QueryRow(ctx, `SELECT id,number,revision,title,description,main_changeset_id,created_at,main_conversation_id,criteria,
		   COALESCE(hitl_mode,'hitl'), COALESCE(merge_mode,'manual')
		FROM repomesh_issues.issues WHERE project_id=$1 AND id=$2 AND removed_at IS NULL`,
		projectID, issueID).Scan(&detail.ID, &detail.Number, &detail.Revision, &detail.Title, &detail.Description, &detail.MainChangeSetID, &detail.CreatedAt, &conversationID, &criteria, &detail.HitlMode, &detail.MergeMode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueDetail{}, failure(404, "RESOURCE_NOT_FOUND")
		}
		return IssueDetail{}, unavailable()
	}
	detail.ProjectID = projectID
	detail.Source = IssueSource{Kind: "issue_page", ConversationID: conversationID}
	if err := json.Unmarshal(criteria, &detail.AcceptanceCriteria); err != nil || detail.AcceptanceCriteria == nil {
		detail.AcceptanceCriteria = []string{}
	}
	detail.RepositoryIDs, err = issueRepositoryIDs(ctx, tx, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	return detail, nil
}

// RoomObservation is the main-room projection (creation contract §7). B07
// shipped the unavailable baseline; the room is now driven by real evidence
// (repository_teams.team_room_id, read back from the AgentTeams Team CR after
// the team was created), and stays unavailable/NOT_ASSOCIATED when there is
// genuinely no room — never a fabricated preparing or ready.
type RoomObservation struct {
	ConversationID string     `json:"conversationId"`
	Availability   string     `json:"availability"`
	Reason         string     `json:"reason"`
	RoomID         *string    `json:"roomId"`
	CanEnter       bool       `json:"canEnter"`
	ObservedAt     *time.Time `json:"observedAt"`
	// RepositoryID / RepositoryName 只有真有房时才带：房间是仓库团队粒度的，界面要能
	// 标出"这是哪个仓的房"。没有房就留空——不拿范围里第一个仓库冒充。
	RepositoryID   string `json:"repositoryId,omitempty"`
	RepositoryName string `json:"repositoryName,omitempty"`
}

// RoomsView is the rooms query projection.
type RoomsView struct {
	IssueID string          `json:"issueId"`
	Main    RoomObservation `json:"main"`
	Leaders []any           `json:"leaders"`
}

// issueRoom 是 issue 范围里某个仓库关联到的 AgentTeams 房。
type issueRoom struct {
	repositoryID   string
	repositoryName string
	roomID         string
}

// RepositoryRoom 是 leaders[] 里的一条：哪个仓库、哪间房、能不能进。
type RepositoryRoom struct {
	RepositoryID   string  `json:"repositoryId"`
	RepositoryName string  `json:"repositoryName,omitempty"`
	ReadOnly       bool    `json:"readOnly"`
	Availability   string  `json:"availability"`
	RoomID         *string `json:"roomId"`
	CanEnter       bool    `json:"canEnter"`
}

// GetIssueRooms 返回一个 issue 的房间关联观察。
//
// 两个层次分清楚：
//   - **关联**（有没有房）来自 `repository_teams.team_room_id` —— 那是建团队之后回读
//     一次 AgentTeams Team CR 的 status 存下来的真实房间号，不是推导出来的。
//   - **就绪**（能不能进）只在真有房间号时才为真。会话存在本身从来不构成
//     preparing/ready 依据（契约 §7 明文），所以没有房间号时仍然如实返
//     unavailable/NOT_ASSOCIATED，绝不编一个出来。
//
// 房间是**仓库团队**粒度的，不是 per-issue 的：上游那间房属于"这个仓库的队伍"，
// 一次 issue 的活会在这几间房里发生。所以 main 取范围里第一个真有房的仓库，
// 其余进 leaders[]，每条都带 repositoryId —— 谁是哪间房，界面能自己标。
func (s *Service) GetIssueRooms(ctx context.Context, principal access.ProjectPrincipal, projectID, issueID string) (RoomsView, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return RoomsView{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return RoomsView{}, err
	}
	if _, err := readProjectRow(ctx, tx, projectID, principal.ActorID()); err != nil {
		return RoomsView{}, err
	}
	var conversationID string
	err = tx.QueryRow(ctx, `SELECT main_conversation_id FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL`, projectID, issueID).Scan(&conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RoomsView{}, failure(404, "RESOURCE_NOT_FOUND")
		}
		return RoomsView{}, unavailable()
	}
	rooms, err := loadIssueRooms(ctx, tx, projectID, issueID)
	if err != nil {
		return RoomsView{}, err
	}
	view := RoomsView{IssueID: issueID, Leaders: []any{}}
	view.Main = RoomObservation{
		ConversationID: conversationID,
		Availability:   "unavailable",
		Reason:         "NOT_ASSOCIATED",
	}
	for index, room := range rooms {
		roomID := room.roomID
		observation := RoomObservation{
			ConversationID: conversationID,
			Availability:   "ready",
			RoomID:         &roomID,
			CanEnter:       true,
			RepositoryID:   room.repositoryID,
			RepositoryName: room.repositoryName,
		}
		if index == 0 {
			view.Main = observation
			continue
		}
		view.Leaders = append(view.Leaders, RepositoryRoom{
			RepositoryID:   room.repositoryID,
			RepositoryName: room.repositoryName,
			Availability:   "ready",
			RoomID:         &roomID,
			CanEnter:       true,
			ReadOnly:       true,
		})
	}
	return view, nil
}

// loadIssueRooms 按 issue 的仓库范围找出真有房的仓库，顺序稳定（按 repository_id），
// 否则同一份数据两次请求可能给出不同的 main。
//
// 一并带出仓库名：界面对 `repository_name === null` 的呈现是「catalog 未收录」，
// 一句我们占不了的说法 —— 名字拿不到就说拿不到，能拿到就别让它去说那句假话。
//
// 0057 起 repository_teams 键为 (project_id, 扫描侧 repository_id)，而 issue 范围里
// 是项目侧 repo_… id —— 先按 URL 对齐把两边认上（与 repositoryteams 的解析同规则），
// 直接 join 永远命不中。仓库名取扫描侧的名字。
func loadIssueRooms(ctx context.Context, tx pgx.Tx, projectID, issueID string) ([]issueRoom, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.repository_id, COALESCE(repo.name, ''), COALESCE(NULLIF(t.team_room_id, ''), '')
		FROM repomesh_issues.issue_repository_scope s
		JOIN repomesh_projects.project_repositories pr
		  ON pr.project_id = s.project_id AND pr.repository_id = s.repository_id
		JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
		JOIN repomesh_projects.projects p ON p.id = pr.project_id
		JOIN repomesh_access.accounts a ON a.id = p.owner
		LEFT JOIN LATERAL (
		  SELECT scan.id, scan.name FROM repomesh_scan.repositories scan
		  WHERE scan.organization_id = a.organization_id
		    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
		      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
		  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
		) repo ON true
		LEFT JOIN public.repository_teams t
		  ON t.project_id = s.project_id AND t.repository_id = repo.id
		WHERE s.project_id = $1 AND s.issue_id = $2
		ORDER BY s.repository_id`, projectID, issueID)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	rooms := []issueRoom{}
	for rows.Next() {
		var room issueRoom
		if err := rows.Scan(&room.repositoryID, &room.repositoryName, &room.roomID); err != nil {
			return nil, unavailable()
		}
		if room.roomID != "" {
			rooms = append(rooms, room)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable()
	}
	return rooms, nil
}
