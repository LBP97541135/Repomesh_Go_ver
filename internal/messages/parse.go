package messages

import (
	"encoding/json"
	"unicode/utf8"
)

func parseSubmitInput(body []byte) (submitInput, error) {
	if len(body) == 0 || !utf8.Valid(body) {
		return submitInput{}, failure(400, "INVALID_JSON")
	}
	if hasDuplicateKeys(body) {
		return submitInput{}, failure(400, "INVALID_JSON")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return submitInput{}, failure(400, "INVALID_JSON")
	}
	known := map[string]bool{"body": true, "replyTo": true}
	for key := range raw {
		if !known[key] {
			return submitInput{}, fieldFailure(string(key), "UNKNOWN_FIELD")
		}
	}
	input := submitInput{}
	if _, ok := raw["body"]; !ok {
		return submitInput{}, fieldFailure("body", "REQUIRED")
	}
	if err := json.Unmarshal(raw["body"], &input.body); err != nil {
		return submitInput{}, fieldFailure("body", "INVALID_TYPE")
	}
	if !utf8.ValidString(input.body) || utf8.RuneCountInString(input.body) > 20000 {
		return submitInput{}, fieldFailure("body", "INVALID_LENGTH")
	}
	blank := true
	for _, character := range input.body {
		if character != ' ' && character != '\t' && character != '\n' && character != '\r' {
			blank = false
			break
		}
	}
	if blank {
		return submitInput{}, fieldFailure("body", "REQUIRED")
	}
	if rawReply, ok := raw["replyTo"]; ok {
		if err := json.Unmarshal(rawReply, &input.replyTo); err != nil {
			return submitInput{}, fieldFailure("replyTo", "INVALID_TYPE")
		}
	}
	return input, nil
}

func canonicalizeSubmit(input submitInput, command SubmissionCommand) []byte {
	document := map[string]any{}
	document["body"] = input.body
	if input.replyTo != "" {
		document["replyTo"] = input.replyTo
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil
	}
	return canonical
}
