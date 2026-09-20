package observeui

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

var errPaidPending = errors.New("previous paid attempt has an unknown or unfinished outcome; inspect it before explicitly starting a new attempt")

type PaidAttempt struct {
	ID        string    `json:"id"`
	ResultID  string    `json:"result_id"`
	Kind      string    `json:"kind"`
	Model     string    `json:"model"`
	InputRef  string    `json:"input_ref"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

// Called while the server's evaluation mutex is held. Stable input identity
// deduplicates copied archives too. An explicit new key means an intentional
// re-evaluation; replaying that key never starts another paid request.
func (s *Server) reservePaid(kind, model, inputRef, subject, key string) (PaidAttempt, bool, error) {
	identity := strings.Join([]string{kind, subject, key}, "\x00")
	if key == "" {
		identity = strings.Join([]string{kind, subject, inputRef, model}, "\x00")
	}
	id := observepipe.Digest([]byte(identity))
	j := s.archives[0].Journal
	var old PaidAttempt
	if err := j.ReadRecord("paid_attempts", id, &old); err == nil {
		if old.ID != id || old.InputRef != inputRef || old.Model != model || old.Kind != kind {
			return old, true, errors.New("attempt key was reused for different evidence or model")
		}
		return old, true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return old, false, err
	}
	s.modelMu.Lock()
	policy, err := s.loadPolicy()
	s.modelMu.Unlock()
	if err != nil {
		return old, false, err
	}
	rows, err := j.Records("paid_attempts")
	if err != nil {
		return old, false, err
	}
	now := time.Now().UTC()
	used := 0
	for _, raw := range rows {
		var attempt PaidAttempt
		if json.Unmarshal(raw, &attempt) != nil {
			return old, false, errors.New("paid attempt archive corrupt")
		}
		if attempt.CreatedAt.UTC().Format("2006-01-02") == now.Format("2006-01-02") {
			used++
		}
	}
	if used >= policy.DailyCallLimit {
		return old, false, errors.New("local daily paid-call limit reached")
	}
	claim := PaidAttempt{ID: id, ResultID: newRecordID(), Kind: kind, Model: model, InputRef: inputRef, Subject: subject, CreatedAt: now}
	if claim.ResultID == "" {
		return old, false, errors.New("cannot allocate paid attempt identity")
	}
	if err := j.PutRecord("paid_attempts", id, claim); err != nil {
		return claim, false, err
	}
	return claim, false, nil
}
