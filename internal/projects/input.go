package projects

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/jsoninput"
)

func ParseRawInput(data []byte) (RawInput, error) {
	data = bytes.TrimSpace(data)
	if jsoninput.Validate(data, 256*1024) != nil {
		return RawInput{}, failure(400, "INVALID_JSON")
	}
	result := RawInput{data: append([]byte(nil), data...)}
	if len(data) > 0 && data[0] == '{' {
		if json.Unmarshal(data, &result.fields) != nil || result.fields == nil {
			return RawInput{}, failure(400, "INVALID_JSON")
		}
	}
	return result, nil
}

func PrepareCreate(input RawInput, key string) (CreateCommand, error) {
	key = strings.ToLower(key)
	if !validKey(key) {
		return CreateCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	return CreateCommand{key: key, input: input}, nil
}

func PrepareUpdate(input RawInput, projectID, key string) (UpdateCommand, error) {
	key = strings.ToLower(key)
	if !validKey(key) {
		return UpdateCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	if !validResourceID(projectID) {
		return UpdateCommand{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	return UpdateCommand{projectID: projectID, key: key, input: input}, nil
}

func parseCreate(input RawInput, schemaVersion int) (CreateInput, normalizedInput, error) {
	if schemaVersion != 1 || input.fields == nil {
		return CreateInput{}, normalizedInput{}, validation("")
	}
	if err := rejectUnknownFields(input.fields, []string{"name", "purpose", "repositoryIds", "configuration"}); err != nil {
		return CreateInput{}, normalizedInput{}, err
	}
	var result CreateInput
	var err error
	if _, ok := input.fields["name"]; !ok {
		return CreateInput{}, normalizedInput{}, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "name", Code: "REQUIRED"}}}
	}
	if result.Name, err = parseText(input.fields["name"], 200); err != nil {
		return CreateInput{}, normalizedInput{}, validation("name")
	}
	if _, ok := input.fields["purpose"]; !ok {
		return CreateInput{}, normalizedInput{}, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "purpose", Code: "REQUIRED"}}}
	}
	if result.Purpose, err = parseText(input.fields["purpose"], 20000); err != nil {
		return CreateInput{}, normalizedInput{}, validation("purpose")
	}
	if _, ok := input.fields["repositoryIds"]; !ok {
		return CreateInput{}, normalizedInput{}, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "repositoryIds", Code: "REQUIRED"}}}
	}
	if result.RepositoryIDs, err = parseRepositoryIDs(input.fields["repositoryIds"], false); err != nil {
		return CreateInput{}, normalizedInput{}, err
	}
	if raw, ok := input.fields["configuration"]; ok {
		if result.Configuration, err = parseConfiguration(raw); err != nil {
			return CreateInput{}, normalizedInput{}, err
		}
	} else {
		result.Configuration = ConfigurationChoice{ModelProfile: ProfileChoice{Mode: "inherit"}, ExecutionProfile: ProfileChoice{Mode: "inherit"}}
	}
	normalized, err := normalizeCreate(result, schemaVersion)
	return result, normalized, err
}

func parseUpdate(input RawInput, schemaVersion int) (UpdateInput, normalizedInput, error) {
	if schemaVersion != 1 || input.fields == nil {
		return UpdateInput{}, normalizedInput{}, validation("")
	}
	if err := rejectUnknownFields(input.fields, []string{"expectedProjectRevision", "name", "purpose", "repositoryIdsToAdd", "repositoryUrlsToAdd", "configuration"}); err != nil {
		return UpdateInput{}, normalizedInput{}, err
	}
	if len(input.fields) < 2 {
		return UpdateInput{}, normalizedInput{}, validation("")
	}
	var result UpdateInput
	var err error
	if _, ok := input.fields["expectedProjectRevision"]; !ok {
		return UpdateInput{}, normalizedInput{}, &Failure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: []FieldError{{Field: "expectedProjectRevision", Code: "REQUIRED"}}}
	}
	if result.ExpectedProjectRevision, err = parseOpaqueID(input.fields["expectedProjectRevision"]); err != nil || !validKey(strings.ToLower(result.ExpectedProjectRevision)) {
		return UpdateInput{}, normalizedInput{}, validation("expectedProjectRevision")
	}
	result.ExpectedProjectRevision = strings.ToLower(result.ExpectedProjectRevision)
	if raw, ok := input.fields["name"]; ok {
		value, err := parseText(raw, 200)
		if err != nil {
			return UpdateInput{}, normalizedInput{}, validation("name")
		}
		result.Name = &value
	}
	if raw, ok := input.fields["purpose"]; ok {
		value, err := parseText(raw, 20000)
		if err != nil {
			return UpdateInput{}, normalizedInput{}, validation("purpose")
		}
		result.Purpose = &value
	}
	if raw, ok := input.fields["repositoryIdsToAdd"]; ok {
		value, err := parseRepositoryIDs(raw, false)
		if err != nil {
			return UpdateInput{}, normalizedInput{}, validation("repositoryIdsToAdd")
		}
		result.RepositoryIDsToAdd = &value
	}
	if raw, ok := input.fields["repositoryUrlsToAdd"]; ok {
		value, err := parseRepositoryURLs(raw)
		if err != nil {
			return UpdateInput{}, normalizedInput{}, validation("repositoryUrlsToAdd")
		}
		result.RepositoryURLsToAdd = &value
	}
	if raw, ok := input.fields["configuration"]; ok {
		value, err := parseConfiguration(raw)
		if err != nil {
			return UpdateInput{}, normalizedInput{}, err
		}
		result.Configuration = &value
	}
	normalized, err := normalizeUpdate(result, schemaVersion)
	return result, normalized, err
}

func parseConfiguration(raw json.RawMessage) (ConfigurationChoice, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || rejectUnknownFields(fields, []string{"modelProfile", "executionProfile"}) != nil || len(fields) != 2 {
		return ConfigurationChoice{}, validation("configuration")
	}
	model, err := parseProfileChoice(fields["modelProfile"])
	if err != nil {
		return ConfigurationChoice{}, err
	}
	execution, err := parseProfileChoice(fields["executionProfile"])
	if err != nil {
		return ConfigurationChoice{}, err
	}
	return ConfigurationChoice{ModelProfile: model, ExecutionProfile: execution}, nil
}

func parseProfileChoice(raw json.RawMessage) (ProfileChoice, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || rejectUnknownFields(fields, []string{"mode", "id"}) != nil {
		return ProfileChoice{}, validation("configuration")
	}
	var mode string
	if json.Unmarshal(fields["mode"], &mode) != nil {
		return ProfileChoice{}, validation("configuration")
	}
	switch mode {
	case "inherit":
		if len(fields) != 1 {
			return ProfileChoice{}, validation("configuration")
		}
		return ProfileChoice{Mode: mode}, nil
	case "reference":
		if len(fields) != 2 {
			return ProfileChoice{}, validation("configuration")
		}
		id, err := parseOpaqueID(fields["id"])
		if err != nil {
			return ProfileChoice{}, validation("configuration")
		}
		return ProfileChoice{Mode: mode, ID: id}, nil
	default:
		return ProfileChoice{}, validation("configuration")
	}
}

func parseRepositoryIDs(raw json.RawMessage, required bool) ([]string, error) {
	var values []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 100 || required && len(values) == 0 {
		return nil, validation("repositoryIds")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, rawID := range values {
		id, err := parseOpaqueID(rawID)
		if err != nil || !strings.HasPrefix(id, "repo_") || len(id) != 25 || seen[id] {
			return nil, validation("repositoryIds")
		}
		seen[id] = true
		result = append(result, id)
	}
	return result, nil
}

// parseRepositoryURLs 解析 `repositoryUrlsToAdd`：一串仓库 URL。
//
// 只做**形状**校验（github.com + owner/name），不在这里访问网络 ——「这个仓存不存在、
// 你有没有权」由后面的参与权观测回答（ObserveProjectRepositoriesFresh），失败也在
// 那个语义下报错，而不是在这里冒充一个 422。
func parseRepositoryURLs(raw json.RawMessage) ([]string, error) {
	var values []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil || values == nil || len(values) == 0 || len(values) > 100 {
		return nil, validation("repositoryUrlsToAdd")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, rawURL := range values {
		// 用 parseText 而不是 parseOpaqueID：后者走 validResourceID，校验的是
		// **资源 id 的形状**（小写字母数字加下划线的定长串）——URL 根本过不了它。
		// 这个坑是测试抓出来的：写成 parseOpaqueID 的话，这个通道**永远 422**，
		// 等于修复白做。上限给 2000 只是防炸弹，不是业务约束。
		text, err := parseText(rawURL, 2000)
		if err != nil {
			return nil, validation("repositoryUrlsToAdd")
		}
		if _, _, _, err := ParseRepositoryURL(text); err != nil {
			return nil, validation("repositoryUrlsToAdd")
		}
		if seen[text] {
			continue
		}
		seen[text] = true
		result = append(result, text)
	}
	if len(result) == 0 {
		return nil, validation("repositoryUrlsToAdd")
	}
	return result, nil
}

func parseText(raw json.RawMessage, maximum int) (string, error) {
	var value string
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maximum {
		return "", validation("")
	}
	return value, nil
}

func parseOpaqueID(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || !validResourceID(value) {
		return "", validation("")
	}
	return value, nil
}

func rejectUnknownFields(fields map[string]json.RawMessage, allowed []string) error {
	for field := range fields {
		found := false
		for _, candidate := range allowed {
			if field == candidate {
				found = true
				break
			}
		}
		if !found {
			return validation("")
		}
	}
	return nil
}

func normalizeCreate(input CreateInput, schemaVersion int) (normalizedInput, error) {
	exact, err := json.Marshal(input)
	if err != nil {
		return normalizedInput{}, unavailable()
	}
	canonicalInput := input
	canonicalInput.RepositoryIDs = append([]string(nil), input.RepositoryIDs...)
	sort.Strings(canonicalInput.RepositoryIDs)
	canonical, err := json.Marshal(canonicalInput)
	if err != nil {
		return normalizedInput{}, unavailable()
	}
	return normalizedInput{schemaVersion: schemaVersion, canonical: canonical, exact: exact}, nil
}

func normalizeUpdate(input UpdateInput, schemaVersion int) (normalizedInput, error) {
	exact, err := json.Marshal(input)
	if err != nil {
		return normalizedInput{}, unavailable()
	}
	canonicalInput := input
	if input.RepositoryIDsToAdd != nil {
		values := append([]string(nil), (*input.RepositoryIDsToAdd)...)
		sort.Strings(values)
		canonicalInput.RepositoryIDsToAdd = &values
	}
	canonical, err := json.Marshal(canonicalInput)
	if err != nil {
		return normalizedInput{}, unavailable()
	}
	return normalizedInput{schemaVersion: schemaVersion, canonical: canonical, exact: exact}, nil
}

func sameInput(operation operationRecord, input normalizedInput) bool {
	return operation.schemaVersion == input.schemaVersion && bytes.Equal(operation.canonicalInput, input.canonical)
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
}

func validKey(value string) bool {
	if len(value) != 36 || value != strings.ToLower(value) {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func validResourceID(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 128 || value == "." || value == ".." || strings.ContainsAny(value, "/%\\?#\x00") {
		return false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
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

func ParseListQuery(values url.Values) (ListQuery, error) {
	if err := rejectQueryFields(values, []string{"q", "cursor", "limit"}); err != nil {
		return ListQuery{}, err
	}
	result := ListQuery{Text: values.Get("q"), Cursor: values.Get("cursor"), Limit: 50}
	if !utf8.ValidString(result.Text) || utf8.RuneCountInString(result.Text) > 200 || len(result.Cursor) > 128 {
		return ListQuery{}, validation("")
	}
	if raw, ok := values["limit"]; ok {
		value, err := strconv.Atoi(raw[0])
		if err != nil {
			return ListQuery{}, validation("limit")
		}
		result.Limit = value
	}
	if result.Limit < 1 || result.Limit > 100 {
		return ListQuery{}, validation("limit")
	}
	return result, nil
}

func ParsePageQuery(values url.Values) (PageQuery, error) {
	if err := rejectQueryFields(values, []string{"cursor", "limit"}); err != nil {
		return PageQuery{}, err
	}
	result := PageQuery{Cursor: values.Get("cursor"), Limit: 50}
	if len(result.Cursor) > 128 {
		return PageQuery{}, validation("cursor")
	}
	if raw, ok := values["limit"]; ok {
		value, err := strconv.Atoi(raw[0])
		if err != nil {
			return PageQuery{}, validation("limit")
		}
		result.Limit = value
	}
	if result.Limit < 1 || result.Limit > 100 {
		return PageQuery{}, validation("limit")
	}
	return result, nil
}

func ParseProfileQuery(values url.Values) (ProfileQuery, error) {
	if err := rejectQueryFields(values, []string{"kind", "cursor", "limit"}); err != nil {
		return ProfileQuery{}, err
	}
	pageValues := url.Values{}
	if raw, ok := values["cursor"]; ok {
		pageValues["cursor"] = raw
	}
	if raw, ok := values["limit"]; ok {
		pageValues["limit"] = raw
	}
	page, err := ParsePageQuery(pageValues)
	if err != nil {
		return ProfileQuery{}, err
	}
	kind := values.Get("kind")
	if kind != "model" && kind != "execution" {
		return ProfileQuery{}, validation("kind")
	}
	return ProfileQuery{Kind: kind, Cursor: page.Cursor, Limit: page.Limit}, nil
}

func parseLimit(values url.Values) (int, error) {
	limit := 50
	if raw, ok := values["limit"]; ok {
		value, err := strconv.Atoi(raw[0])
		if err != nil {
			return 0, validation("limit")
		}
		limit = value
	}
	if limit < 1 || limit > 100 {
		return 0, validation("limit")
	}
	return limit, nil
}

func rejectQueryFields(values url.Values, allowed []string) error {
	for key, entries := range values {
		if len(entries) != 1 {
			return validation(key)
		}
		found := false
		for _, candidate := range allowed {
			if key == candidate {
				found = true
				break
			}
		}
		if !found {
			return validation(key)
		}
	}
	return nil
}
