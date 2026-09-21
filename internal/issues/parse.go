package issues

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// parseNewInput parses and validates a first-attempt request body. Unknown fields
// are 422; duplicate JSON keys and invalid Unicode are 400.
// issueCheckpoints 是六个人工卡点，与 humancontrol 的项目监管策略**逐字同一组**
// （internal/humancontrol/policy.go 的 policyCheckpoints）。两处各写一份是因为
// 这两个包之间没有依赖；值必须一致，改一处就要改另一处。
var issueCheckpoints = []string{
	"repository_scope", "specification", "execution",
	"validation", "delivery", "exception_escalation",
}

var issueCheckpointSet = func() map[string]bool {
	set := map[string]bool{}
	for _, name := range issueCheckpoints {
		set[name] = true
	}
	return set
}()

// allIssueCheckpoints 返回六个卡点的副本（调用方会持有它，不能给共享切片）。
func allIssueCheckpoints() []string {
	out := make([]string, len(issueCheckpoints))
	copy(out, issueCheckpoints)
	return out
}

// executionTierToHitl 把三档翻译成自动托管循环用的两值 hitl_mode。
//
// auto / supervised → 'ai'：发现链自动推进（半自动的"停"由它自己的卡点决定，
// 不是整条链都停）；manual_controlled → 'hitl'：发现链的门等真人。
// 保留 hitl_mode 是因为自动托管循环与既有读面都在用它，改由档位派生即可。
func executionTierToHitl(executionMode string) string {
	if executionMode == "manual_controlled" {
		return "hitl"
	}
	return "ai"
}

func parseNewInput(body []byte) (pageInput, error) {
	if len(body) == 0 || !utf8.Valid(body) {
		return pageInput{}, failure(400, "INVALID_JSON")
	}
	if hasDuplicateKeys(body) {
		return pageInput{}, failure(400, "INVALID_JSON")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return pageInput{}, failure(400, "INVALID_JSON")
	}

	known := map[string]bool{}
	known["expectedCreationContextRevision"] = true
	known["description"] = true
	known["title"] = true
	known["repositoryIds"] = true
	known["acceptanceCriteria"] = true
	known["conversation"] = true
	known["repositoryAnalysisId"] = true
	known["hitlMode"] = true
	known["mergeMode"] = true
	for key := range raw {
		if !known[key] {
			return pageInput{}, failure(422, "VALIDATION_FAILED")
		}
	}
	input := pageInput{}
	context, err := requiredString(raw, "expectedCreationContextRevision", 128)
	if err != nil {
		return pageInput{}, err
	}
	input.expectedContext = context
	title, err := requiredText(raw, "title", 200)
	if err != nil {
		return pageInput{}, err
	}
	input.title = title
	description, err := requiredText(raw, "description", 20000)
	if err != nil {
		return pageInput{}, err
	}
	input.description = description
	repositories, present, err := listValue(raw, "repositoryIds", 100, 64, true, true)
	if err != nil {
		return pageInput{}, err
	}
	// 2026-09-20（用户："需求不写仓库为什么就不行？"）：仓库范围**不再必填**。
	// 需求里没点名仓库时，候选评分那一步会退到本项目的全部仓库目录，由 Manager
	// （总领导）自己去发现该改哪些仓（见 internal/discovery/recall.go 的 loadRepoPool）。
	// 缺字段与空数组是同一个意思：这次没点名。
	_ = present
	input.repositories = repositories
	criteria, _, err := listValue(raw, "acceptanceCriteria", 100, 2000, true, false)
	if err != nil {
		return pageInput{}, err
	}
	input.criteria = criteria
	conversation, err := parseConversation(raw)
	if err != nil {
		return pageInput{}, err
	}
	input.conversation = conversation
	analysisID, present, err := optionalString(raw, "repositoryAnalysisId", 128)
	if err != nil {
		return pageInput{}, err
	}
	if present {
		if analysisID == "" {
			return pageInput{}, fieldFailure("repositoryAnalysisId", "INVALID_ID")
		}
		input.analysisID = &analysisID
	}
	// 人工参与 / 自动托管：服务端事实，不再只活在浏览器 sessionStorage 里
	// （协调器的自动托管循环此前无条件代行 ③ 审批与 ⑤ 物化，人工参与模式下也一样）。
	mode, present, err := optionalString(raw, "hitlMode", 8)
	if err != nil {
		return pageInput{}, err
	}
	if present {
		switch mode {
		case "ai", "hitl":
			input.hitlMode = mode
		default:
			return pageInput{}, fieldFailure("hitlMode", "INVALID_VALUE")
		}
	} else {
		// 缺省最保守：门等真人，不替任何人做主。
		input.hitlMode = "hitl"
	}
	// 合并方式：同样落成服务端事实。缺省 manual —— 合并是唯一的外部副作用
	// （真动用户仓库、真进主分支），没有明说要自动合并的就不替任何人合。
	mergeMode, mergePresent, err := optionalString(raw, "mergeMode", 8)
	if err != nil {
		return pageInput{}, err
	}
	if mergePresent {
		switch mergeMode {
		case "auto", "manual":
			input.mergeMode = mergeMode
		default:
			return pageInput{}, fieldFailure("mergeMode", "INVALID_VALUE")
		}
	}
	// 注意：mergeMode 缺省时**不再兜底成 manual** —— 它现在从档位卡点派生
	// （见 service.go 的 mergeModeFromCheckpoints）：勾了「交付」卡点就等人点合并，
	// 没勾就交付闸门一开自动合。这里留空 = 没说，派生说了算。
	// 监管强度三档 + 半自动自选卡点（迁移 0065）。
	//
	// 域不变量照抄既有的项目监管策略（internal/humancontrol/policy.go），不另立一套：
	//   auto 卡点必须为空 / supervised 至少一个 / manual_controlled 正好六个。
	// 这里先按应用层判一遍，存储层那条 CHECK 是第二道 —— 两道都要，因为绕过应用的
	// 写路径同样能造出"auto 带卡点"这种自相矛盾的行。
	//
	// 兼容：老客户端只发 hitlMode 时，按它如实翻译成对应档位（ai→auto / hitl→manual），
	// 不改变既有调用方的实际行为。
	checkpoints, _, err := listValue(raw, "requiredCheckpoints", 6, 64, true, true)
	if err != nil {
		return pageInput{}, err
	}
	for _, checkpoint := range checkpoints {
		if !issueCheckpointSet[checkpoint] {
			return pageInput{}, fieldFailure("requiredCheckpoints", "INVALID_VALUE")
		}
	}
	mode, modePresent, err := optionalString(raw, "executionMode", 24)
	if err != nil {
		return pageInput{}, err
	}
	if !modePresent {
		if input.hitlMode == "ai" {
			input.executionMode = "auto"
		} else {
			input.executionMode = "manual_controlled"
			checkpoints = allIssueCheckpoints()
		}
	} else {
		switch mode {
		case "auto", "supervised", "manual_controlled":
			input.executionMode = mode
		default:
			return pageInput{}, fieldFailure("executionMode", "INVALID_VALUE")
		}
	}
	switch input.executionMode {
	case "auto":
		if len(checkpoints) > 0 {
			return pageInput{}, fieldFailure("requiredCheckpoints", "AUTO_MUST_NOT_REQUIRE_CHECKPOINTS")
		}
	case "supervised":
		if len(checkpoints) == 0 {
			return pageInput{}, fieldFailure("requiredCheckpoints", "SUPERVISED_REQUIRES_CHECKPOINT")
		}
	case "manual_controlled":
		// 人工审核 = 六个卡点全要。客户端可以不发（省一次往返），这里补齐。
		checkpoints = allIssueCheckpoints()
	}
	input.requiredCheckpoints = checkpoints
	return input, nil
}

func optionalString(raw map[string]json.RawMessage, key string, max int) (string, bool, error) {
	message, ok := raw[key]
	if !ok {
		return "", false, nil
	}
	if bytes.Equal(bytes.TrimSpace(message), []byte("null")) {
		return "", false, fieldFailure(key, "INVALID_TYPE")
	}
	var value string
	if err := json.Unmarshal(message, &value); err != nil {
		return "", false, fieldFailure(key, "INVALID_TYPE")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return "", false, fieldFailure(key, "TOO_LONG")
	}
	for _, character := range value {
		// 换行/制表/回车是合法文本(契约:description 上限 20000,显式多行);
		// 其余 C0 控制字符与 DEL 照禁。
		if (character < 32 && character != '\n' && character != '\t' && character != '\r') || character == 127 {
			return "", false, fieldFailure(key, "INVALID_CHARACTER")
		}
	}
	return value, true, nil
}

func requiredString(raw map[string]json.RawMessage, key string, max int) (string, error) {
	value, present, err := optionalString(raw, key, max)
	if err != nil {
		return "", err
	}
	if !present || value == "" {
		return "", fieldFailure(key, "REQUIRED")
	}
	return value, nil
}

func requiredText(raw map[string]json.RawMessage, key string, max int) (string, error) {
	value, err := requiredString(raw, key, max)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fieldFailure(key, "REQUIRED")
	}
	return value, nil
}

func listValue(raw map[string]json.RawMessage, key string, maxItems, maxRunes int, nonBlank, unique bool) ([]string, bool, error) {
	message, ok := raw[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(message), []byte("null")) {
		return nil, false, fieldFailure(key, "INVALID_TYPE")
	}
	var items []string
	if err := json.Unmarshal(message, &items); err != nil {
		return nil, false, fieldFailure(key, "INVALID_TYPE")
	}
	if len(items) > maxItems {
		return nil, false, fieldFailure(key, "TOO_MANY")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !utf8.ValidString(item) || utf8.RuneCountInString(item) > maxRunes {
			return nil, false, fieldFailure(key, "TOO_LONG")
		}
		if nonBlank && strings.TrimSpace(item) == "" {
			return nil, false, fieldFailure(key, "REQUIRED")
		}
		if unique && seen[item] {
			return nil, false, fieldFailure(key, "DUPLICATE")
		}
		seen[item] = true
	}
	return items, true, nil
}

func parseConversation(raw map[string]json.RawMessage) (conversationChoice, error) {
	message, ok := raw["conversation"]
	if !ok {
		return newConversation{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(message, &fields); err != nil || fields == nil {
		return nil, fieldFailure("conversation", "INVALID_TYPE")
	}
	modeMessage, ok := fields["mode"]
	if !ok {
		return nil, fieldFailure("conversation", "REQUIRED")
	}
	var mode string
	if err := json.Unmarshal(modeMessage, &mode); err != nil {
		return nil, fieldFailure("conversation", "INVALID_TYPE")
	}
	switch mode {
	case "new":
		if len(fields) != 1 {
			return nil, fieldFailure("conversation", "UNEXPECTED_FIELD")
		}
		return newConversation{}, nil
	case "existing":
		if len(fields) != 2 {
			return nil, fieldFailure("conversation", "UNEXPECTED_FIELD")
		}
		id, present, err := optionalString(fields, "id", 64)
		if err != nil || !present || id == "" {
			return nil, fieldFailure("conversation", "INVALID_ID")
		}
		return existingConversation{id: id}, nil
	default:
		return nil, fieldFailure("conversation", "INVALID_MODE")
	}
}

// hasDuplicateKeys reports whether the document repeats an object key anywhere,
// which the contract treats as malformed JSON rather than a validation failure.
func hasDuplicateKeys(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var walk func() bool
	walk = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return false
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return false
				}
				key, ok := keyToken.(string)
				if !ok {
					return false
				}
				if seen[key] {
					return true
				}
				seen[key] = true
				if walk() {
					return true
				}
			}
			_, err := decoder.Token()
			return err != nil
		case '[':
			for decoder.More() {
				if walk() {
					return true
				}
			}
			_, err := decoder.Token()
			return err != nil
		}
		return false
	}
	return walk()
}

// canonicalize renders the normalized input that equality is decided on:
// schemaVersion 1, sorted repositories, empty acceptance arrays spelled out,
// a new conversation written as {"mode":"new"}.
func canonicalize(input pageInput) []byte {
	conversation := map[string]any{"mode": input.conversation.conversationMode()}
	if existing, ok := input.conversation.(existingConversation); ok {
		conversation["id"] = existing.id
	}
	repositories := append([]string(nil), input.repositories...)
	sort.Strings(repositories)
	criteria := input.criteria
	if criteria == nil {
		criteria = []string{}
	}
	document := map[string]any{}
	document["schemaVersion"] = 1
	document["expectedCreationContextRevision"] = input.expectedContext
	document["title"] = input.title
	document["description"] = input.description
	document["repositoryIds"] = repositories
	document["acceptanceCriteria"] = criteria
	document["conversation"] = conversation
	// 人审门模式进指纹：同一个幂等键换模式重放是一次**不同的**建项请求，
	// 不能拿旧回执当"已创建"糊过去。
	document["hitlMode"] = input.hitlMode
	if input.analysisID != nil {
		document["repositoryAnalysisId"] = *input.analysisID
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil
	}
	return canonical
}
