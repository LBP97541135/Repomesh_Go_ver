package scan

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCatalog stores repository cards in repomesh_scan.repositories.
// The AutoCard payload in the metadata column is written and read only via
// autoCardPayload/autoCardFromPayload — the single serializer.
type PostgresCatalog struct {
	pool *pgxpool.Pool
}

func NewPostgresCatalog(pool *pgxpool.Pool) *PostgresCatalog {
	return &PostgresCatalog{pool: pool}
}

const cardColumns = `id, name, url, description, topics, languages,
	fingerprint, profiled_at, metadata, test_commands, test_paths`

func (c *PostgresCatalog) Add(ctx context.Context, card RepositoryCard) error {
	payload, err := json.Marshal(metadataPayload(card.AutoCard, card.ObservedCalls))
	if err != nil {
		return err
	}
	_, err = c.pool.Exec(ctx, `
		INSERT INTO repomesh_scan.repositories
		    (id, name, url, description, topics, languages,
		     fingerprint, profiled_at, metadata, test_commands, test_paths)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, $9, $10)`,
		card.ID, card.Name, card.URL, card.Description,
		jsonSlice(card.Topics), jsonSlice(card.Languages),
		card.Fingerprint, payload, jsonSlice(card.TestCommands), jsonSlice(card.TestPaths))
	return err
}

func (c *PostgresCatalog) List(ctx context.Context) ([]RepositoryCard, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT `+cardColumns+` FROM repomesh_scan.repositories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []RepositoryCard{}
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *card)
	}
	return cards, rows.Err()
}

// nullableOrg 把空串落成 SQL NULL（NULL 表示"没有空间归属"）。
func nullableOrg(organizationID string) any {
	if organizationID == "" {
		return nil
	}
	return organizationID
}

// AddInOrganization 登记一张卡片并**盖上空间归属**。
//
// 2026-09-19 账号隔离：repomesh_scan.repositories.organization_id 这一列一直
// 存在，但从来没人写过 —— 于是扫描目录对所有人可见（公有部署下就是泄漏）。
// 写入路径必须盖章，读取路径才能裁剪。
func (c *PostgresCatalog) AddInOrganization(ctx context.Context, organizationID string, card RepositoryCard) error {
	payload, err := json.Marshal(metadataPayload(card.AutoCard, card.ObservedCalls))
	if err != nil {
		return err
	}
	_, err = c.pool.Exec(ctx, `
		INSERT INTO repomesh_scan.repositories
		    (id, name, url, description, topics, languages,
		     fingerprint, profiled_at, metadata, test_commands, test_paths, organization_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, $9, $10, $11::uuid)`,
		card.ID, card.Name, card.URL, card.Description,
		jsonSlice(card.Topics), jsonSlice(card.Languages),
		card.Fingerprint, payload, jsonSlice(card.TestCommands), jsonSlice(card.TestPaths),
		nullableOrg(organizationID))
	return err
}

// StampOrganization 给既有行补章（刷新路径：老行可能没有空间归属）。
// 只补 NULL，不覆盖已有归属——同一仓库被两个空间各自扫描时，先到先得。
func (c *PostgresCatalog) StampOrganization(ctx context.Context, id, organizationID string) error {
	if organizationID == "" {
		return nil
	}
	_, err := c.pool.Exec(ctx,
		`UPDATE repomesh_scan.repositories SET organization_id=$2::uuid WHERE id=$1 AND organization_id IS NULL`,
		id, organizationID)
	return err
}

// ListInOrganization 只返回属于该空间的卡片。
//
// 调用者**没有空间**时返回空集，而不是退化成"全部"——与名册的处理一致：
// 宁可少给，也不越权多给。
func (c *PostgresCatalog) ListInOrganization(ctx context.Context, organizationID string) ([]RepositoryCard, error) {
	if organizationID == "" {
		return []RepositoryCard{}, nil
	}
	rows, err := c.pool.Query(ctx,
		`SELECT `+cardColumns+` FROM repomesh_scan.repositories WHERE organization_id=$1::uuid ORDER BY name`,
		organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []RepositoryCard{}
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *card)
	}
	return cards, rows.Err()
}

func (c *PostgresCatalog) Get(ctx context.Context, id string) (*RepositoryCard, error) {
	card, err := c.queryCard(ctx,
		`SELECT `+cardColumns+` FROM repomesh_scan.repositories WHERE id = $1`, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return card, err
}

func (c *PostgresCatalog) GetByName(ctx context.Context, name string) (*RepositoryCard, error) {
	card, err := c.queryCard(ctx,
		`SELECT `+cardColumns+` FROM repomesh_scan.repositories WHERE name = $1`, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return card, err
}

// UpdateAutoCard whole-replaces the scan-derived card: metadata and
// fingerprint change, operator-owned fields do not. Idempotent.
func (c *PostgresCatalog) UpdateAutoCard(ctx context.Context, id string, card AutoCard, languages []string, fingerprint string) error {
	payload, err := json.Marshal(autoCardPayload(card))
	if err != nil {
		return err
	}
	// The new payload is the base; the observed block is carried over from
	// the old metadata so a scan refresh never wipes runtime evidence.
	tag, err := c.pool.Exec(ctx, `
		UPDATE repomesh_scan.repositories
		SET metadata = jsonb_set($2::jsonb, '{observedCalls}',
			COALESCE(metadata->'observedCalls', '[]'::jsonb), true),
		    languages = $3, fingerprint = $4, profiled_at = now()
		WHERE id = $1`, id, payload, jsonSlice(languages), fingerprint)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (c *PostgresCatalog) queryCard(ctx context.Context, sql string, args ...any) (*RepositoryCard, error) {
	row := c.pool.QueryRow(ctx, sql, args...)
	card, err := scanCard(row)
	if err != nil {
		return nil, err
	}
	return card, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanCard(row rowScanner) (*RepositoryCard, error) {
	var (
		card                                     RepositoryCard
		topics, langs, commands, paths, metadata []byte
		profiledAt                               time.Time
	)
	if err := row.Scan(&card.ID, &card.Name, &card.URL, &card.Description,
		&topics, &langs, &card.Fingerprint, &profiledAt,
		&metadata, &commands, &paths); err != nil {
		return nil, err
	}
	card.Topics = jsonToSlice(topics)
	card.Languages = jsonToSlice(langs)
	card.TestCommands = jsonToSlice(commands)
	card.TestPaths = jsonToSlice(paths)
	card.ProfiledAt = profiledAt.UTC().Format(time.RFC3339)

	var payload map[string]any
	if err := json.Unmarshal(metadata, &payload); err != nil {
		return nil, err
	}
	card.AutoCard = autoCardFromPayload(payload)
	card.ObservedCalls = observedCallsFromPayload(payload)
	return &card, nil
}

func cardMetadata(card *AutoCard) map[string]any {
	if card == nil {
		return map[string]any{}
	}
	return autoCardPayload(*card)
}

// metadataPayload assembles the metadata document: the card payload plus,
// when present, the runtime-observed call block (mechanism 6).
func metadataPayload(card *AutoCard, observed []ObservedCall) map[string]any {
	payload := cardMetadata(card)
	if len(observed) > 0 {
		payload["observedCalls"] = observed
	}
	return payload
}

// observedCallsFromPayload decodes the runtime-observation block back into
// typed calls; absent or malformed yields nil — honest "nothing observed".
func observedCallsFromPayload(raw map[string]any) []ObservedCall {
	items, ok := raw["observedCalls"].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	calls := make([]ObservedCall, 0, len(items))
	for _, item := range items {
		table, ok := item.(map[string]any)
		if !ok {
			continue
		}
		target, _ := table["target"].(string)
		count := 0
		if number, ok := table["calls"].(float64); ok {
			count = int(number)
		}
		calls = append(calls, ObservedCall{Target: target, Calls: count})
	}
	return calls
}

// ReplaceObservedCalls writes the runtime-observed call block (mechanism 6
// import). The block is a sibling of the card inside metadata: a re-scan
// refresh must not erase it, and a re-import replaces it wholesale.
func (c *PostgresCatalog) ReplaceObservedCalls(ctx context.Context, id string, calls []ObservedCall) error {
	encoded, err := json.Marshal(calls)
	if err != nil {
		return err
	}
	tag, err := c.pool.Exec(ctx, `
		UPDATE repomesh_scan.repositories
		SET metadata = jsonb_set(metadata, '{observedCalls}', $2, true)
		WHERE id = $1`, id, encoded)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func jsonSlice(values []string) []byte {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil { // unreachable for []string
		return []byte("[]")
	}
	return encoded
}

func jsonToSlice(raw []byte) []string {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return []string{}
	}
	return values
}
