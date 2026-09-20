package discovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"repomesh.local/repomesh/internal/observability"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/secrets"
)

// llmVerdict 是模型对"这个仓库是否需要为这条需求改动"的判断。
//
// 2026-09-19：这是 GOAI-infra-repomesh 设计里"总 Manager 全局扫描"的落地——
// 把**所有仓库的名片（含 AutoCard）**拼进 prompt，让模型按语义判断相关性，
// 而不是拿需求里的词去仓库名上做字符串命中。
type llmVerdict struct {
	Repository string  `json:"repository"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`

	// 以下是图推理（第二层）打上的标注，不来自模型。
	Matched            []string `json:"-"`
	FromGraph          bool     `json:"-"`
	ExcludedByGraph    bool     `json:"-"`
	ConflictsWithGraph bool     `json:"-"`
}

// containsFold 是大小写无关的子串判断（关键词回退路径用）。
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func joinStrings(values []string, sep string) string { return strings.Join(values, sep) }

// providerEndpoint 是从 repomesh_models 解析出来的出站模型端点。
type providerEndpoint struct {
	ProviderID string
	BaseURL    string
	APIFormat  string
	ModelID    string
	SecretRef  string
}

// resolveProvider 取部署里第一个启用的供应商及其第一个模型。
//
// discovery 没有"项目级模型档案"（那是智能体运行面的概念），所以用部署默认：
// 与"总 Manager 用部署配置的模型做全局召回"语义一致。取不到就返回 error，
// 调用方回退关键词路径。
func (s *Service) resolveProvider(ctx context.Context, tx pgx.Tx, projectID string) (*providerEndpoint, error) {
	const query = `
		SELECT p.id, pr.base_url, pr.api_format, ms.model_id, pr.secret_version_id
		FROM repomesh_models.providers p
		JOIN repomesh_models.provider_revisions pr
		  ON pr.provider_id = p.id AND pr.revision = p.head_revision
		JOIN repomesh_models.model_snapshots ms
		  ON ms.provider_id = pr.provider_id AND ms.provider_revision = pr.revision
		WHERE p.enabled AND p.owner=(SELECT owner FROM repomesh_projects.projects WHERE id=$1)
		ORDER BY p.id, ms.row_id
		LIMIT 1`
	endpoint := &providerEndpoint{}
	err := tx.QueryRow(ctx, query, projectID).Scan(&endpoint.ProviderID, &endpoint.BaseURL, &endpoint.APIFormat, &endpoint.ModelID, &endpoint.SecretRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("discovery: 未配置可用的模型供应商（请先在「模型」页保存一个中转站）")
	}
	if err != nil {
		return nil, err
	}
	if endpoint.BaseURL == "" || endpoint.ModelID == "" || endpoint.SecretRef == "" {
		return nil, errors.New("discovery: 模型供应商配置不完整")
	}
	return endpoint, nil
}

const recallSystemPrompt = `你是仓库发现器。给定一条需求，判断此 Issue 已确认范围内哪些仓库需要改动。

规则：
1. 只依据给出的仓库名片判断，**不要臆造仓库**；仓库名必须逐字来自输入。
2. 置信度 confidence 是 0 到 1 的小数：
   - 0.7 及以上：有正面证据（共享依赖、接口、目录/模块职责直接对应需求）
   - 0.4 到 0.7：可能间接涉及
   - 0.4 以下：无关，**不要输出**
3. rationale 用一句中文写清判断依据，引用你看到的具体事实（依赖名、目录、接口等）。
4. 只输出 JSON，不要任何解释文字。`

// semanticRecall 调模型做语义召回。任何一步失败都返回 error —— 由调用方回退到
// 关键词路径并如实标注 llm_used=false，绝不假装模型给过分。
func (s *Service) semanticRecall(ctx context.Context, tx pgx.Tx, projectID, issueID, requirement string, cards []repoCard) ([]llmVerdict, error) {
	if s.secrets == nil {
		return nil, errors.New("discovery: 未接入密钥存储，无法调用模型")
	}
	if len(cards) == 0 {
		return nil, errors.New("discovery: 候选池为空")
	}
	endpoint, err := s.resolveProvider(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	key, err := s.secrets.Open(ctx, secrets.VersionID(endpoint.SecretRef),
		secrets.Owner{Kind: "model-provider", ID: endpoint.ProviderID}, secrets.ModelProviderKey)
	if err != nil {
		return nil, fmt.Errorf("discovery: 解封模型密钥失败: %w", err)
	}

	var b strings.Builder
	b.WriteString("需求：\n")
	b.WriteString(strings.TrimSpace(requirement))
	b.WriteString("\n\n仓库名片：\n")
	for _, card := range cards {
		b.WriteString("- ")
		b.WriteString(cardText(card))
		b.WriteString("\n")
	}
	b.WriteString("\n请输出 JSON：{\"candidates\":[{\"repository\":\"owner/name\",\"confidence\":0.0,\"rationale\":\"...\"}]}")

	payload, err := json.Marshal(map[string]any{
		"model":       endpoint.ModelID,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": recallSystemPrompt},
			{"role": "user", "content": b.String()},
		},
	})
	if err != nil {
		return nil, err
	}
	target := strings.TrimRight(endpoint.BaseURL, "/") + "/chat/completions"
	sendCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(sendCtx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(key))
	client := s.client()
	callID := make([]byte, 16)
	if _, err := rand.Read(callID); err != nil {
		return nil, errors.New("discovery: cannot allocate model call identity")
	}
	started := time.Now()
	observation := observability.ModelCall{ID: hex.EncodeToString(callID), ProjectID: projectID, IssueID: issueID, ProviderID: endpoint.ProviderID, RequestedModel: endpoint.ModelID, StartedAt: started.UTC(), Status: "error"}
	defer func() {
		observation.DurationNS = time.Since(started).Nanoseconds()
		if s.modelObserver != nil {
			s.modelObserver(observation)
		}
	}()
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("discovery: 调用模型失败: %w", err)
	}
	defer response.Body.Close()
	observation.HTTPStatus = response.StatusCode
	observation.ProviderRequestID = response.Header.Get("x-request-id")
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("discovery: 模型返回 HTTP %d：%s", response.StatusCode, truncate(string(raw), 200))
	}
	var decoded struct {
		Model string `json:"model"`
		ID    string `json:"id"`
		Usage *struct {
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
			CacheHit   *int64 `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Choices) == 0 {
		return nil, errors.New("discovery: 模型响应无法解析")
	}
	observation.ResponseModel = decoded.Model
	if observation.ProviderRequestID == "" {
		observation.ProviderRequestID = decoded.ID
	}
	if decoded.Usage != nil {
		observation.InputTokens = nonnegativeUsage(decoded.Usage.Prompt)
		observation.OutputTokens = nonnegativeUsage(decoded.Usage.Completion)
		observation.CacheReadTokens = nonnegativeUsage(decoded.Usage.CacheHit)
	}
	observation.Status = "ok"
	observation.ResultStatus = "invalid_output"
	verdicts, err := parseVerdicts(decoded.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	// 幻觉过滤：模型只能从名片里挑仓库，挑出不存在的一律丢掉。
	known := map[string]bool{}
	for _, card := range cards {
		known[strings.ToLower(card.Name)] = true
	}
	filtered := verdicts[:0]
	for _, verdict := range verdicts {
		if known[strings.ToLower(strings.TrimSpace(verdict.Repository))] {
			filtered = append(filtered, verdict)
		}
	}
	observation.ResultStatus = "used"
	observation.ResultUsed = true
	if len(filtered) == 0 {
		observation.ResultStatus = "filtered_empty"
		observation.ResultUsed = false
	}
	return filtered, nil
}

// parseVerdicts 容错解析：模型可能把 JSON 包在 markdown fence 里，或前后带解释文字。
func parseVerdicts(content string) ([]llmVerdict, error) {
	text := strings.TrimSpace(content)
	if start := strings.IndexAny(text, "{["); start > 0 {
		text = text[start:]
	}
	if end := strings.LastIndexAny(text, "}]"); end >= 0 && end+1 <= len(text) {
		text = text[:end+1]
	}
	var wrapper struct {
		Candidates []llmVerdict `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &wrapper); err == nil && len(wrapper.Candidates) > 0 {
		return wrapper.Candidates, nil
	}
	var bare []llmVerdict
	if err := json.Unmarshal([]byte(text), &bare); err == nil && len(bare) > 0 {
		return bare, nil
	}
	return nil, fmt.Errorf("discovery: 模型输出不是可解析的候选 JSON：%s", truncate(content, 160))
}

func (s *Service) client() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return &http.Client{Timeout: 90 * time.Second}
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func nonnegativeUsage(n *int64) *int64 {
	if n == nil || *n < 0 {
		return nil
	}
	value := *n
	return &value
}
