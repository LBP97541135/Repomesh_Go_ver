package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// modeljudge.go 是 A/B 评估的**模型侧**：两臂各自作答 + 盲裁判选优。
//
// 为什么必须走模型（2026-09-21 用户裁定）：上一版执行器拿**技能正文自己**当
// "带技能臂的答案"，再去正文里找测试题要求的关键词 —— 那是在量"文档里写没写这个词"，
// 不是量"agent 拿到这个技能会不会做得更好"。判定恒偏向正文长的一方，对照组恒为空串，
// 结论恒为 win。用户原话："ab测试不应该是agent盲选吗"。所以这里改成：
//
//	① 两臂用**同一个模型、同一个系统提示**，唯一差别是**有没有把技能正文放进上下文**；
//	② 两份答案按**随机盲标**（A/B）交给裁判模型，裁判**看不到哪份用了技能**；
//	③ 反盲后才把分数归到 with / without 两臂上。
//
// 说明清楚它不是什么：裁判是模型，不是人；分数是**主观序**，不是客观正确率。
// 它挡得住"技能没带来增量"，挡不住"模型自己偏心长答案"。所以盲标、同题、同模型
// 这三个约束必须都成立，否则这份记录没有意义。

// ABJudge 是 A/B 评估用到的模型能力面。抽成接口是为了让执行器可测：
// 测试里塞一个确定性假裁判，线上塞真实中转站。
type ABJudge interface {
	// Name 是写进 `skill_evaluation_runs.judged_by` 的判定者标识。
	// 必须能一眼看出"这是模型盲评"还是"这是本地覆盖度检查"。
	Name() string
	// Answer 出一份答案。skillContent 为空 = 不带技能臂（对照组）。
	Answer(ctx context.Context, skillContent, question string) (string, error)
	// Judge 盲裁判：slotA / slotB 是**已经打乱**的两份答案，调用方负责记住
	// 哪个槽位对应哪条臂；这里只回 A / B / tie 与各自分数。
	Judge(ctx context.Context, question, slotA, slotB string) (BlindVerdict, error)
}

// BlindVerdict 是裁判对两份匿名答案的判断。
type BlindVerdict struct {
	Winner    string  `json:"winner"` // "A" | "B" | "tie"
	ScoreA    float64 `json:"score_a"`
	ScoreB    float64 `json:"score_b"`
	Rationale string  `json:"rationale"`
}

// ChatEndpoint 是从部署里的中转站配置解析出来的出站端点。
type ChatEndpoint struct {
	ProviderID      string
	BaseURL         string
	APIFormat       string
	ModelID         string
	SecretVersionID string
}

// FirstEnabledChatEndpoint 取部署里第一个启用的 openai_chat_completions 中转站。
//
// preferModel 非空时优先选 model_id 等于它的那条（用户指定"用 tokendance 的
// deepseek-v4.1-flash"）；匹配不到就按 row_id 取第一条 —— 绝不臆造端点。
func (s *Store) FirstEnabledChatEndpoint(ctx context.Context, preferModel string) (*ChatEndpoint, error) {
	const query = `
		SELECT p.id, pr.base_url, pr.api_format, ms.model_id, pr.secret_version_id
		FROM repomesh_models.providers p
		JOIN repomesh_models.provider_revisions pr
		  ON pr.provider_id = p.id AND pr.revision = p.head_revision
		JOIN repomesh_models.model_snapshots ms
		  ON ms.provider_id = pr.provider_id AND ms.provider_revision = pr.revision
		WHERE p.enabled
		  AND pr.api_format = 'openai_chat_completions'
		  AND COALESCE(pr.secret_version_id, '') <> ''
		  AND ($1 = '' OR ms.model_id = $1)
		ORDER BY ms.row_id
		LIMIT 1`
	endpoint := &ChatEndpoint{}
	err := s.Pool.QueryRow(ctx, query, preferModel).Scan(
		&endpoint.ProviderID, &endpoint.BaseURL, &endpoint.APIFormat,
		&endpoint.ModelID, &endpoint.SecretVersionID)
	if err != nil {
		if preferModel != "" {
			// 指定模型不存在时退回"任意一条"，但**不静默**：错误里带上原委。
			if fallback, ferr := s.FirstEnabledChatEndpoint(ctx, ""); ferr == nil {
				return fallback, nil
			}
		}
		return nil, fmt.Errorf("skills: 部署里没有可用的中转站（需要一条启用的 openai_chat_completions 供应商）: %w", err)
	}
	if endpoint.BaseURL == "" || endpoint.ModelID == "" || endpoint.SecretVersionID == "" {
		return nil, errors.New("skills: 中转站配置不完整（base_url / model_id / secret 缺一不可）")
	}
	return endpoint, nil
}

// ChatModel 是 ABJudge 的真实实现：OpenAI chat-completions 兼容端点。
//
// AnswerModel 与 JudgeModel **默认是同一个模型**（部署里通常只有一个中转站）。
// 用户若给了两个模型，就自动变成"生成用 A、裁判用 B" —— 避免自己评自己。
// 这不是硬要求，但值得在记录里写明用了哪个（Name() 带裁判模型名）。
type ChatModel struct {
	BaseURL     string
	APIKey      string
	AnswerModel string
	JudgeModel  string
	HTTP        *http.Client

	// ProviderLabel 只用于展示，例如 "tokendance.space"。
	ProviderLabel string
}

func (m *ChatModel) Name() string {
	label := strings.TrimSpace(m.ProviderLabel)
	if label == "" {
		label = "chat"
	}
	return fmt.Sprintf("blind_llm_judge:%s:%s", label, m.JudgeModel)
}

const abAnswerSystemPrompt = `你是一名资深软件工程助手。请**直接作答**，不要寒暄、不要复述题目。
若提供了「技能规范」，严格按它的流程与约束作答。只输出答案正文。`

const abJudgeSystemPrompt = `你是**盲评裁判**。你会看到同一个问题的两份匿名答案（甲 / 乙）。
你不知道它们分别由谁产生，也不要去猜。

评分维度（各 0-10 分）：
1. 是否直接回答了问题；
2. 是否给出了可执行的具体步骤或结论，而不是空泛原则；
3. 是否覆盖了该问题的关键风险与边界。

只输出 JSON，不要任何解释文字：
{"winner":"A"|"B"|"tie","score_a":0-10,"score_b":0-10,"rationale":"一句中文说明"}`

// Answer 生成一份答案。skillContent 为空 = 对照组（不给技能）。
//
// 两臂**系统提示完全相同**，唯一变量是用户消息里有没有技能正文 —— 否则
// 差异会混进提示本身，A/B 就不成立了。
func (m *ChatModel) Answer(ctx context.Context, skillContent, question string) (string, error) {
	var user strings.Builder
	if strings.TrimSpace(skillContent) != "" {
		user.WriteString("【技能规范】\n")
		user.WriteString(strings.TrimSpace(skillContent))
		user.WriteString("\n\n")
	}
	user.WriteString("【问题】\n")
	user.WriteString(strings.TrimSpace(question))
	return m.complete(ctx, m.AnswerModel, abAnswerSystemPrompt, user.String())
}

// Judge 盲裁判。slotA / slotB 是打乱后的两份答案。
func (m *ChatModel) Judge(ctx context.Context, question, slotA, slotB string) (BlindVerdict, error) {
	var user strings.Builder
	user.WriteString("【问题】\n")
	user.WriteString(strings.TrimSpace(question))
	user.WriteString("\n\n【答案甲】\n")
	user.WriteString(strings.TrimSpace(slotA))
	user.WriteString("\n\n【答案乙】\n")
	user.WriteString(strings.TrimSpace(slotB))
	user.WriteString("\n\n请按 JSON 输出判断。")
	raw, err := m.complete(ctx, m.JudgeModel, abJudgeSystemPrompt, user.String())
	if err != nil {
		return BlindVerdict{}, err
	}
	verdict, err := parseBlindVerdict(raw)
	if err != nil {
		return BlindVerdict{}, err
	}
	return verdict, nil
}

// parseBlindVerdict 容错解析裁判输出（模型常把 JSON 包在 ``` 里或前后带话）。
func parseBlindVerdict(content string) (BlindVerdict, error) {
	text := strings.TrimSpace(content)
	if start := strings.Index(text, "{"); start > 0 {
		text = text[start:]
	}
	if end := strings.LastIndex(text, "}"); end >= 0 && end+1 <= len(text) {
		text = text[:end+1]
	}
	var v BlindVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return BlindVerdict{}, fmt.Errorf("skills: 裁判输出无法解析: %w", err)
	}
	v.Winner = strings.ToLower(strings.TrimSpace(v.Winner))
	switch v.Winner {
	case "a", "甲":
		v.Winner = "A"
	case "b", "乙":
		v.Winner = "B"
	case "tie", "平局", "equal":
		v.Winner = "tie"
	default:
		// 模型给了没见过的取值 —— 按 tie 记，**不猜**。
		v.Winner = "tie"
	}
	if v.ScoreA < 0 || v.ScoreA > 10 {
		v.ScoreA = clampScore(v.ScoreA)
	}
	if v.ScoreB < 0 || v.ScoreB > 10 {
		v.ScoreB = clampScore(v.ScoreB)
	}
	return v, nil
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

// complete 是唯一的出站点：一次请求，一次解析，失败如实报错。
func (m *ChatModel) complete(ctx context.Context, model, system, user string) (string, error) {
	if strings.TrimSpace(m.BaseURL) == "" || strings.TrimSpace(m.APIKey) == "" || strings.TrimSpace(model) == "" {
		return "", errors.New("skills: 模型裁判未配置（base_url / api_key / model 缺一不可）")
	}
	payload, err := json.Marshal(map[string]any{
		"model":       model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", err
	}
	target := strings.TrimRight(m.BaseURL, "/") + "/chat/completions"
	sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(sendCtx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+m.APIKey)
	client := m.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("skills: 调用模型失败: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("skills: 模型返回 HTTP %d：%s", response.StatusCode, truncateForError(string(raw), 200))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Choices) == 0 {
		return "", errors.New("skills: 模型响应无法解析")
	}
	return decoded.Choices[0].Message.Content, nil
}

func truncateForError(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

// SecretOpener 解封一条密钥版本的明文。由调用方注入（web 侧是 secrets.Store，
// 批处理侧可以只给环境变量）。
type SecretOpener func(ctx context.Context, secretVersionID, providerID string) (string, error)

// ABJudgeOptions 是构造模型裁判的全部外部输入。
//
// 取值顺序（**环境变量优先，部署里的中转站兜底**）：先看 BaseURL/APIKey/Model
// 是否给全了；给全了就直接用，**不碰数据库**。否则从 `repomesh_models` 里取一条
// 启用的 openai_chat_completions 供应商，用 SecretOpener 解封它的密钥。
// 两条路都不通就返回 error —— 调用方据此**不注入 Judge**，评估端点会如实拒绝。
type ABJudgeOptions struct {
	BaseURL    string
	APIKey     string
	Model      string
	JudgeModel string
	// PreferModel 按 model_id 指定优先使用哪个模型（例如 tokendance 上的
	// deepseek-v4.1-flash）；匹配不到会退回"任意一条启用的"。
	PreferModel string
	// Label 只进 judged_by，便于在库里区分来源（例如 "tokendance.space"）。
	Label string
}

// OpenABJudge 组装模型裁判。返回的第二个值是一句**如实的来源说明**，供启动日志。
func OpenABJudge(ctx context.Context, store *Store, open SecretOpener, opts ABJudgeOptions) (ABJudge, string, error) {
	baseURL := strings.TrimSpace(opts.BaseURL)
	apiKey := strings.TrimSpace(opts.APIKey)
	model := strings.TrimSpace(opts.Model)
	judgeModel := strings.TrimSpace(opts.JudgeModel)
	source := "环境变量"
	if baseURL == "" || apiKey == "" || model == "" {
		if store == nil || open == nil {
			return nil, "", errors.New("skills: 未配置模型裁判（环境变量不全，且没有可用的密钥存储去读部署里的中转站）")
		}
		// 选哪条中转站：PreferModel 优先；没给就用 Model 本身当偏好 ——
		// 否则"只指定了模型名、没给 key"时，会挑到**第一条**启用供应商的
		// base_url/key，却仍然拿这个模型名去请求，等于把模型名发给了不认识的网关。
		prefer := strings.TrimSpace(opts.PreferModel)
		if prefer == "" {
			prefer = model
		}
		endpoint, err := store.FirstEnabledChatEndpoint(ctx, prefer)
		if err != nil {
			return nil, "", err
		}
		key, err := open(ctx, endpoint.SecretVersionID, endpoint.ProviderID)
		if err != nil {
			return nil, "", fmt.Errorf("skills: 解封中转站密钥失败: %w", err)
		}
		baseURL, apiKey = endpoint.BaseURL, key
		if model == "" {
			model = endpoint.ModelID
		}
		source = fmt.Sprintf("部署中转站 %s（%s）", endpoint.ProviderID, endpoint.BaseURL)
	}
	if judgeModel == "" {
		// 没给独立裁判模型时，生成与裁判同模型。**不是**"自己评自己"——
		// 裁判看不到哪份答案来自哪条臂，同模型只是省一份配置。
		judgeModel = model
	}
	label := strings.TrimSpace(opts.Label)
	if label == "" {
		label = hostOf(baseURL)
	}
	return &ChatModel{
		BaseURL: baseURL, APIKey: apiKey,
		AnswerModel: model, JudgeModel: judgeModel,
		ProviderLabel: label,
	}, fmt.Sprintf("%s · answer=%s · judge=%s", source, model, judgeModel), nil
}

// hostOf 从 base_url 里取主机名，只用于展示标签。
func hostOf(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	if idx := strings.IndexByte(trimmed, '/'); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if trimmed == "" {
		return "chat"
	}
	return trimmed
}
