package issues

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"repomesh.local/repomesh/internal/projects"
)

// Failure is the typed service error the web layer maps onto the contract error
// envelope.
type Failure struct {
	Status int
	Code   string
	Fields []projects.FieldError
	// Cause 是触发这次失败的底层错误（例如 Postgres 报的编码/约束/超时）。
	//
	// 2026-09-20 线上实测：unavailable() 把底层错误整个丢掉，日志里只剩
	// "issues: RESULT_UNCONFIRMED"，503 查了一整轮都指不到真正的原因 —— 实际是
	// 一条 INSERT 撞了 "invalid byte sequence for encoding UTF8: 0x00"。
	// Cause 只进服务端日志（writeProjectError 打印 err），不进响应体。
	Cause error
}

func (f *Failure) Error() string {
	if f.Cause != nil {
		return fmt.Sprintf("issues: %s: %v", f.Code, f.Cause)
	}
	if len(f.Fields) > 0 {
		return fmt.Sprintf("issues: %s (%s)", f.Code, f.Fields[0].Field)
	}
	return "issues: " + f.Code
}

func failure(status int, code string) error {
	return &Failure{Status: status, Code: code}
}

func fieldFailure(field, code string) error {
	return &Failure{Status: 422, Code: "VALIDATION_FAILED", Fields: []projects.FieldError{{Field: field, Code: code}}}
}

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

// unavailableWith 与 unavailable 同样的状态码，但保留底层错误供日志排障。
func unavailableWith(cause error) error {
	if cause == nil {
		return unavailable()
	}
	return &Failure{Status: 503, Code: "RESULT_UNCONFIRMED", Cause: cause}
}

// digest is the server-side input fingerprint; the client's own digest is never
// trusted, so this always recomputes from the canonical input.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// newID mints one opaque identifier from cryptographic randomness. Identifiers
// are never derived from user input.
func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", unavailable()
	}
	return prefix + hex.EncodeToString(buffer), nil
}
