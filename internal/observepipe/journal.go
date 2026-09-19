package observepipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxRecordBytes = 32 << 20

// Journal is a private, append-only local archive. Consumers acknowledge
// individual event IDs, not a monotonic source cursor (commits can arrive late).
type Journal struct{ Dir string }

type diskRecord struct {
	Checksum string          `json:"sha256"`
	Data     json.RawMessage `json:"data"`
}

func OpenJournal(dir string) (*Journal, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("archive directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("archive must be a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("archive directory must be private (mode 0700)")
	}
	return &Journal{Dir: abs}, nil
}

// PutJSON keeps exact canonical JSON as content-addressed evidence.
func (j *Journal) PutJSON(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return j.PutRawJSON(b)
}

// PutRawJSON preserves exact source bytes as well as a content hash. In
// particular it must not compact a database snapshot and break its fingerprint.
func (j *Journal) PutRawJSON(b []byte) (string, error) {
	if !json.Valid(b) {
		return "", errors.New("invalid evidence JSON")
	}
	if len(b) > maxRecordBytes {
		return "", errors.New("evidence exceeds archive record limit")
	}
	ref := "evidence/" + Digest(b) + ".json"
	if err := j.immutableWrite(ref, b); err != nil {
		return "", err
	}
	return ref, nil
}

func (j *Journal) ReadEvidence(ref string, dest any) error {
	if !validRef(ref, "evidence") {
		return errors.New("invalid evidence reference")
	}
	b, err := j.read(ref)
	if err != nil {
		return err
	}
	if Digest(b) != strings.TrimSuffix(filepath.Base(ref), ".json") {
		return errors.New("evidence checksum mismatch")
	}
	return json.Unmarshal(b, dest)
}

func validRef(ref, bucket string) bool {
	if filepath.ToSlash(ref) != ref || !strings.HasPrefix(ref, bucket+"/") {
		return false
	}
	name := strings.TrimPrefix(ref, bucket+"/")
	if len(name) != 69 || !strings.HasSuffix(name, ".json") {
		return false
	}
	for _, c := range name[:64] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (j *Journal) Append(e Event) (Event, bool, error) {
	if err := e.Validate(); err != nil {
		return Event{}, false, err
	}
	for _, ref := range append(append([]string{e.ContextManifestRef}, e.EvidenceRefs...), e.InputArtifactRefs...) {
		var body json.RawMessage
		if err := j.ReadEvidence(ref, &body); err != nil {
			return Event{}, false, fmt.Errorf("event evidence unavailable: %w", err)
		}
	}
	ref := "events/" + e.EventID + ".json"
	existing, err := j.ReadEvent(e.EventID)
	if err == nil {
		if existing.semanticFingerprint() != e.semanticFingerprint() {
			return Event{}, false, errors.New("source identity was reused with different facts")
		}
		return existing, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Event{}, false, err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return Event{}, false, err
	}
	record, err := json.Marshal(diskRecord{Checksum: Digest(data), Data: data})
	if err != nil {
		return Event{}, false, err
	}
	if err = j.immutableWrite(ref, record); err != nil {
		// Concurrent collection can generate different technical IDs; recover the
		// winner only when every source fact is identical.
		winner, readErr := j.ReadEvent(e.EventID)
		if readErr == nil && winner.semanticFingerprint() == e.semanticFingerprint() {
			return winner, false, nil
		}
		return Event{}, false, err
	}
	return e, true, nil
}

func (j *Journal) ReadEvent(id string) (Event, error) {
	ref := "events/" + id + ".json"
	if !validRef(ref, "events") {
		return Event{}, errors.New("invalid event identity")
	}
	b, err := j.read(ref)
	if err != nil {
		return Event{}, err
	}
	var record diskRecord
	if err = json.Unmarshal(b, &record); err != nil {
		return Event{}, errors.New("invalid event archive")
	}
	if Digest(record.Data) != record.Checksum {
		return Event{}, errors.New("event archive checksum mismatch")
	}
	var e Event
	if err = json.Unmarshal(record.Data, &e); err != nil {
		return Event{}, err
	}
	if e.EventID != id {
		return Event{}, errors.New("event filename does not match identity")
	}
	return e, e.Validate()
}

func (j *Journal) Events() ([]Event, error) {
	paths, err := filepath.Glob(filepath.Join(j.Dir, "events", "*.json"))
	if err != nil {
		return nil, err
	}
	all := make([]Event, 0, len(paths))
	for _, p := range paths {
		e, err := j.ReadEvent(strings.TrimSuffix(filepath.Base(p), ".json"))
		if err != nil {
			return nil, err
		}
		all = append(all, e)
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].OccurredAt.Equal(all[b].OccurredAt) {
			return all[a].EventID < all[b].EventID
		}
		return all[a].OccurredAt.Before(all[b].OccurredAt)
	})
	return all, nil
}

// PutRecord writes an immutable, checksummed administrative record, e.g. an
// export receipt or a platform evaluation. Same identity/different content fails.
func (j *Journal) PutRecord(bucket, identity string, value any) error {
	if !oneOf(bucket, "receipts", "platform", "trials", "spans", "judgments") {
		return errors.New("unsupported record bucket")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b, err := json.Marshal(diskRecord{Checksum: Digest(data), Data: data})
	if err != nil {
		return err
	}
	return j.immutableWrite(bucket+"/"+Digest([]byte(identity))+".json", b)
}

func (j *Journal) ReadRecord(bucket, identity string, dest any) error {
	if !oneOf(bucket, "receipts", "platform", "trials", "spans", "judgments") {
		return errors.New("unsupported record bucket")
	}
	b, err := j.read(bucket + "/" + Digest([]byte(identity)) + ".json")
	if err != nil {
		return err
	}
	var r diskRecord
	if err = json.Unmarshal(b, &r); err != nil {
		return err
	}
	if Digest(r.Data) != r.Checksum {
		return errors.New("record checksum mismatch")
	}
	return json.Unmarshal(r.Data, dest)
}

// Records reads checksummed records without exposing arbitrary file paths.
// Callers must also validate domain identities using ReadRecord.
func (j *Journal) Records(bucket string) ([]json.RawMessage, error) {
	if !oneOf(bucket, "receipts", "platform", "trials", "spans", "judgments") {
		return nil, errors.New("unsupported record bucket")
	}
	paths, err := filepath.Glob(filepath.Join(j.Dir, bucket, "*.json"))
	if err != nil {
		return nil, err
	}
	result := []json.RawMessage{}
	for _, path := range paths {
		b, err := j.read(bucket + "/" + filepath.Base(path))
		if err != nil {
			return nil, err
		}
		var record diskRecord
		if json.Unmarshal(b, &record) != nil || Digest(record.Data) != record.Checksum {
			return nil, errors.New("record checksum mismatch")
		}
		result = append(result, record.Data)
	}
	return result, nil
}

func (j *Journal) immutableWrite(ref string, data []byte) error {
	if len(data) > maxRecordBytes {
		return errors.New("archive record exceeds limit")
	}
	bucket := strings.SplitN(ref, "/", 2)[0]
	if !validRef(ref, bucket) {
		return errors.New("invalid archive reference")
	}
	dir := filepath.Join(j.Dir, bucket)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("archive bucket must be a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("archive bucket must be private (mode 0700)")
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	target := filepath.Join(j.Dir, ref)
	if err = os.Link(f.Name(), target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		existing, readErr := j.read(ref)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(existing, data) {
			return errors.New("immutable archive record conflict")
		}
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (j *Journal) read(ref string) ([]byte, error) {
	bucket := strings.SplitN(ref, "/", 2)[0]
	if !validRef(ref, bucket) {
		return nil, errors.New("invalid archive reference")
	}
	parent, err := os.Lstat(filepath.Join(j.Dir, bucket))
	if err != nil {
		return nil, err
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("archive bucket must be a real directory")
	}
	p := filepath.Join(j.Dir, ref)
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("archive record must be a regular file")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxRecordBytes {
		return nil, errors.New("archive record exceeds limit")
	}
	return b, nil
}
