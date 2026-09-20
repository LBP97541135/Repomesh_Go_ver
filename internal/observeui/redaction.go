package observeui

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

var credentialText = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{12,}|apikey_[a-z0-9_-]{20,}|gh[pousr]_[a-z0-9]{20,}|bearer\s+[a-z0-9._~+/-]{8,})`)
var credentialAssignment = regexp.MustCompile(`(?i)(?:authorization|proxy-authorization|cookie|set-cookie|password|passwd|api[_-]?key|access[_-]?token|refresh[_-]?token)\s*[:=]\s*["']?[^\s,"';}]+`)
var emailText = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
var privateKeyText = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)

func sensitiveField(key string) bool {
	k := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(key))
	return strings.Contains(k, "apikey") || strings.Contains(k, "password") || strings.Contains(k, "passwd") || strings.Contains(k, "authorization") || strings.Contains(k, "accesstoken") || strings.Contains(k, "refreshtoken") || strings.Contains(k, "secret") || strings.Contains(k, "privatekey") || k == "cookie" || k == "setcookie"
}

// Model payloads are a separate projection from original private evidence.
// Embedded JSON strings (GenAI messages/tool arguments) are parsed too, rather
// than relying only on outer JSON key names. Counts make removed data visible.
func sanitizeModelState(state any, keys ...string) map[string]any {
	raw, _ := json.Marshal(state)
	var root any
	_ = json.Unmarshal(raw, &root)
	redactions := 0
	var walk func(any, int) any
	walk = func(v any, depth int) any {
		if depth > 24 {
			redactions++
			return "[redacted: nesting limit]"
		}
		switch x := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for key, value := range x {
				if sensitiveField(key) {
					out[key] = "[redacted]"
					redactions++
				} else {
					out[key] = walk(value, depth+1)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = walk(item, depth+1)
			}
			return out
		case string:
			trim := strings.TrimSpace(x)
			if len(trim) > 1 && (trim[0] == '{' || trim[0] == '[') {
				var embedded any
				if json.Unmarshal([]byte(trim), &embedded) == nil {
					b, _ := json.Marshal(walk(embedded, depth+1))
					return string(b)
				}
			}
			for _, key := range keys {
				if key != "" && strings.Contains(x, key) {
					x = strings.ReplaceAll(x, key, "[redacted]")
					redactions++
				}
			}
			for _, pattern := range []*regexp.Regexp{privateKeyText, credentialText, credentialAssignment, emailText} {
				if pattern.MatchString(x) {
					x = pattern.ReplaceAllString(x, "[redacted]")
					redactions++
				}
			}
			if u, err := url.Parse(x); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
				if u.User != nil {
					u.User = nil
					redactions++
				}
				q := u.Query()
				for key := range q {
					if sensitiveField(key) {
						q.Set(key, "[redacted]")
						redactions++
					}
				}
				u.RawQuery = q.Encode()
				x = u.String()
			}
			return x
		default:
			return v
		}
	}
	clean := walk(root, 0)
	return map[string]any{"evidence": clean, "redaction": map[string]any{"policy": "repomesh-model-payload/1", "removed_fields_or_patterns": redactions, "limitations": "Pattern redaction cannot prove arbitrary prose contains no sensitive information; only selected evidence is sent."}}
}

func (s *Server) modelPayload(state any, key string) map[string]any {
	keys := []string{key}
	s.modelMu.Lock()
	assistant, err := s.loadAssistant()
	s.modelMu.Unlock()
	if err == nil {
		keys = append(keys, assistant.Key)
	}
	grading, err := s.loadModel()
	if err == nil {
		keys = append(keys, grading.Key)
	}
	return sanitizeModelState(state, keys...)
}
