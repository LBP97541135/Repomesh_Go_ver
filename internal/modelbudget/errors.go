package modelbudget

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Failure is the coded error type shared across packages: Status is an HTTP
// projection hint, Code a stable machine-readable code.
type Failure struct {
	Status int
	Code   string
}

func (e *Failure) Error() string { return fmt.Sprintf("modelbudget: %s", e.Code) }

func failure(status int, code string) error { return &Failure{Status: status, Code: code} }

func unavailable() error { return failure(503, "RESULT_UNCONFIRMED") }

// newRevision generates a random uuid-v4-shaped revision for window updates.
func newRevision() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], buf[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], buf[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], buf[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], buf[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], buf[10:16])
	return string(dst), nil
}
