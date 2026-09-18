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
	if !present || len(repositories) == 0 {
		return pageInput{}, fieldFailure("repositoryIds", "REQUIRED")
	}
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
	if input.analysisID != nil {
		document["repositoryAnalysisId"] = *input.analysisID
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil
	}
	return canonical
}