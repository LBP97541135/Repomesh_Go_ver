package models

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/jsoninput"
)

func ReadSaveBody(data []byte) (RawSaveBody, error) {
	data = bytes.TrimSpace(data)
	if jsoninput.Validate(data, 256*1024) != nil || len(data) == 0 || data[0] != '{' {
		return RawSaveBody{}, failure(400, "INVALID_JSON")
	}
	result := RawSaveBody{data: append([]byte(nil), data...)}
	if json.Unmarshal(data, &result.fields) != nil || result.fields == nil {
		return RawSaveBody{}, failure(400, "INVALID_JSON")
	}
	return result, nil
}

func NewSaveCommand(key, requestID string, body RawSaveBody) (SaveCommand, error) {
	key = strings.ToLower(key)
	if !validUUID(key) {
		return SaveCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	if requestID == "" || len(requestID) > 128 {
		requestID = newID()
	}
	return SaveCommand{key: key, requestID: requestID, body: body}, nil
}

func NewCloseCommand(pathKey, headerKey string, body []byte) (CloseCommand, error) {
	pathKey = strings.ToLower(pathKey)
	headerKey = strings.ToLower(headerKey)
	if !validUUID(pathKey) || pathKey != headerKey {
		return CloseCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	body = bytes.TrimSpace(body)
	if jsoninput.Validate(body, 256) != nil || !bytes.Equal(body, []byte("{}")) {
		return CloseCommand{}, failure(400, "INVALID_JSON")
	}
	return CloseCommand{key: pathKey}, nil
}

func ParseListQuery(text, cursor, limit string) (ListQuery, error) {
	result := ListQuery{Text: text, Cursor: cursor, Limit: 50}
	if !utf8.ValidString(result.Text) || utf8.RuneCountInString(result.Text) > 200 || len(result.Cursor) > 128 {
		return ListQuery{}, validation("")
	}
	if limit != "" {
		value, err := strconv.Atoi(limit)
		if err != nil || value < 1 || value > 100 {
			return ListQuery{}, validation("limit")
		}
		result.Limit = value
	}
	return result, nil
}

func parseSave(body RawSaveBody, schemaVersion int) (saveInput, canonicalInput, error) {
	if schemaVersion != 1 || body.fields == nil {
		return saveInput{}, nil, validation("")
	}
	if err := rejectUnknown(body.fields, []string{"providerId", "expectedRevision", "name", "baseUrl", "apiFormat", "secret", "models"}); err != nil {
		return saveInput{}, nil, err
	}
	var input saveInput
	var err error
	if input.providerID, err = parseOptionalUUID(body.fields["providerId"]); err != nil {
		return saveInput{}, nil, validation("providerId")
	}
	if input.expectedRevision, err = parseOptionalUUID(body.fields["expectedRevision"]); err != nil {
		return saveInput{}, nil, validation("expectedRevision")
	}
	if (input.providerID == nil) != (input.expectedRevision == nil) {
		return saveInput{}, nil, validation("expectedRevision")
	}
	if input.name, err = parseText(body.fields["name"], 100); err != nil {
		return saveInput{}, nil, validation("name")
	}
	if input.baseURL, err = parseBaseURL(body.fields["baseUrl"]); err != nil {
		return saveInput{}, nil, validation("baseUrl")
	}
	if input.apiFormat, err = parseText(body.fields["apiFormat"], 64); err != nil || input.apiFormat != "openai_chat_completions" {
		return saveInput{}, nil, validation("apiFormat")
	}
	replace, secret, err := parseSecret(body.fields["secret"])
	if err != nil {
		return saveInput{}, nil, err
	}
	input.replace = replace
	input.secret = secret
	if input.providerID == nil && !replace {
		return saveInput{}, nil, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "secret", Code: "REQUIRED"}}}
	}
	if input.models, err = parseModels(body.fields["models"]); err != nil {
		return saveInput{}, nil, err
	}
	canonical, err := json.Marshal(struct {
		SchemaVersion    int          `json:"schemaVersion"`
		ProviderID       *string      `json:"providerId"`
		ExpectedRevision *string      `json:"expectedRevision"`
		Name             string       `json:"name"`
		BaseURL          string       `json:"baseUrl"`
		APIFormat        string       `json:"apiFormat"`
		Replace          bool         `json:"replace"`
		Models           []ModelInput `json:"models"`
	}{schemaVersion, input.providerID, input.expectedRevision, input.name, input.baseURL, input.apiFormat, input.replace, input.models})
	if err != nil {
		return saveInput{}, nil, unavailable()
	}
	if replace {
		return input, replaceInput{schemaVersion: schemaVersion, canonical: canonical, secret: append([]byte(nil), secret...)}, nil
	}
	return input, keepInput{schemaVersion: schemaVersion, canonical: canonical}, nil
}

func encodeProtectedInput(input canonicalInput) ([]byte, error) {
	var schema int
	var canonical, secret []byte
	switch value := input.(type) {
	case keepInput:
		schema, canonical = value.schemaVersion, value.canonical
	case replaceInput:
		schema, canonical, secret = value.schemaVersion, value.canonical, value.secret
	default:
		return nil, unavailable()
	}
	buffer := make([]byte, 8, 8+len(canonical)+4+len(secret))
	binary.BigEndian.PutUint32(buffer[0:4], uint32(schema))
	binary.BigEndian.PutUint32(buffer[4:8], uint32(len(canonical)))
	buffer = append(buffer, canonical...)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(secret)))
	buffer = append(buffer, length[:]...)
	buffer = append(buffer, secret...)
	return buffer, nil
}

func compareProtectedInput(stored, incoming []byte) error {
	if !bytes.Equal(stored, incoming) {
		return failure(409, "IDEMPOTENCY_CONFLICT")
	}
	return nil
}

func parseSecret(raw json.RawMessage) (bool, []byte, error) {
	if raw == nil {
		return false, nil, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "secret", Code: "REQUIRED"}}}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false, nil, validation("secret")
	}
	mode, err := parseText(fields["mode"], 16)
	if err != nil {
		return false, nil, validation("secret.mode")
	}
	switch mode {
	case "keep":
		if err := rejectUnknown(fields, []string{"mode"}); err != nil {
			return false, nil, validation("secret")
		}
		return false, nil, nil
	case "replace":
		if err := rejectUnknown(fields, []string{"mode", "value"}); err != nil {
			return false, nil, validation("secret")
		}
		var value string
		if json.Unmarshal(fields["value"], &value) != nil || !utf8.ValidString(value) || len(value) < 1 || len(value) > 8192 {
			return false, nil, validation("secret.value")
		}
		return true, []byte(value), nil
	default:
		return false, nil, validation("secret.mode")
	}
}

func parseModels(raw json.RawMessage) ([]ModelInput, error) {
	if raw == nil {
		return nil, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "models", Code: "REQUIRED"}}}
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) < 1 || len(items) > 50 {
		return nil, validation("models")
	}
	result := make([]ModelInput, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) != nil {
			return nil, validation("models")
		}
		if err := rejectUnknown(fields, []string{"id", "modelId", "displayName", "contextWindow", "maxOutputTokens", "reasoning", "vision"}); err != nil {
			return nil, err
		}
		var model ModelInput
		var err error
		if model.ID, err = parseOptionalUUID(fields["id"]); err != nil {
			return nil, validation("models.id")
		}
		if model.ModelID, err = parseText(fields["modelId"], 200); err != nil {
			return nil, validation("models.modelId")
		}
		if seen[model.ModelID] {
			return nil, validation("models.modelId")
		}
		seen[model.ModelID] = true
		if rawName, ok := fields["displayName"]; ok && !bytes.Equal(bytes.TrimSpace(rawName), []byte("null")) {
			name, nameErr := parseText(rawName, 200)
			if nameErr != nil {
				return nil, validation("models.displayName")
			}
			model.DisplayName = &name
		}
		if model.ContextWindow, err = parsePositive(fields["contextWindow"]); err != nil {
			return nil, validation("models.contextWindow")
		}
		if model.MaxOutputTokens, err = parsePositive(fields["maxOutputTokens"]); err != nil || model.MaxOutputTokens > model.ContextWindow {
			return nil, validation("models.maxOutputTokens")
		}
		if model.Reasoning, err = parseBool(fields["reasoning"]); err != nil {
			return nil, validation("models.reasoning")
		}
		if model.Vision, err = parseBool(fields["vision"]); err != nil {
			return nil, validation("models.vision")
		}
		result = append(result, model)
	}
	return result, nil
}

func parseBaseURL(raw json.RawMessage) (string, error) {
	value, err := parseText(raw, 2048)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", validation("baseUrl")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if strings.HasSuffix(path, "/chat/completions") {
		return "", validation("baseUrl")
	}
	canonical := "https://" + parsed.Host + path
	return canonical, nil
}

func parseText(raw json.RawMessage, maximum int) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
		return "", validation("")
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > maximum {
		return "", validation("")
	}
	return value, nil
}

func parseOptionalUUID(raw json.RawMessage) (*string, error) {
	if raw == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, validation("")
	}
	value = strings.ToLower(value)
	if !validUUID(value) {
		return nil, validation("")
	}
	return &value, nil
}

func parsePositive(raw json.RawMessage) (int64, error) {
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return 0, validation("")
	}
	value, err := number.Int64()
	if err != nil || value < 1 || value > 2147483647 {
		return 0, validation("")
	}
	return value, nil
}

func parseBool(raw json.RawMessage) (bool, error) {
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, validation("")
	}
	return value, nil
}

func rejectUnknown(fields map[string]json.RawMessage, allowed []string) error {
	known := map[string]bool{}
	for _, key := range allowed {
		known[key] = true
	}
	for key := range fields {
		if !known[key] {
			return validation(key)
		}
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validVersionUUID(value string) bool {
	return validUUID(value) && value[14] == '4' && (value[19] == '8' || value[19] == '9' || value[19] == 'a' || value[19] == 'b')
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code, FieldErrors: []FieldError{}}
}

func validation(field string) error {
	result := failure(422, "VALIDATION_FAILED").(*Failure)
	if field != "" {
		result.FieldErrors = append(result.FieldErrors, FieldError{Field: field, Code: "INVALID"})
	}
	return result
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

func displayName(input ModelInput) string {
	if input.DisplayName != nil && *input.DisplayName != "" {
		return *input.DisplayName
	}
	return input.ModelID
}

func operationPath(key string) string { return "/api/model-provider-saves/" + key }
func providerPath(id string) string   { return "/api/model-providers/" + id }
