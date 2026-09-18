package scan

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type memoryStore struct {
	mu     sync.Mutex
	rows   map[string]RepositoryCard
	failOn map[string]bool // names whose writes fail
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: map[string]RepositoryCard{}, failOn: map[string]bool{}}
}

func (m *memoryStore) Add(_ context.Context, card RepositoryCard) error {
	if m.failOn[card.Name] {
		return errors.New("injected write failure")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[card.Name] = card
	return nil
}

func (m *memoryStore) List(_ context.Context) ([]RepositoryCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RepositoryCard, 0, len(m.rows))
	for _, row := range m.rows {
		out = append(out, row)
	}
	return out, nil
}

func (m *memoryStore) Get(_ context.Context, id string) (*RepositoryCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.ID == id {
			copy := row
			return &copy, nil
		}
	}
	return nil, nil
}

func (m *memoryStore) GetByName(_ context.Context, name string) (*RepositoryCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[name]; ok {
		copy := row
		return &copy, nil
	}
	return nil, nil
}

func (m *memoryStore) UpdateAutoCard(_ context.Context, id string, card AutoCard, languages []string, fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, row := range m.rows {
		if row.ID != id {
			continue
		}
		if m.failOn[name] {
			return errors.New("injected write failure")
		}
		row.AutoCard = &card
		row.Languages = languages
		row.Fingerprint = fingerprint
		row.ScanStatus = ScanStatusOK
		m.rows[name] = row
		return nil
	}
	return errors.New("row not found")
}

func cardProfile(name, fingerprint string, card *AutoCard) RepositoryCard {
	return RepositoryCard{
		ID:           "id-" + name,
		Name:         name,
		URL:          "https://example.com/" + name,
		Description:  "operator typed this",
		Topics:       []string{"payments"},
		TestCommands: []string{"pytest"},
		AutoCard:     card,
		ScanStatus:   ScanStatusOK,
		Fingerprint:  fingerprint,
	}
}

func TestRegisterScannedFreshInsert(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	counts, err := RegisterScanned(ctx, store, []RepositoryCard{
		cardProfile("a", "sha-a", &AutoCard{Deps: []string{"x"}}),
		cardProfile("b", "sha-b", &AutoCard{Deps: []string{"y"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts != (RegistrationCounts{Total: 2, Registered: 2}) {
		t.Fatalf("counts = %+v, want 2 registered", counts)
	}
}

func TestRescanRefreshesExistingRow(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	if _, err := RegisterScanned(ctx, store,
		[]RepositoryCard{cardProfile("order", "sha-1", &AutoCard{Deps: []string{"old"}})}); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetByName(ctx, "order")

	rescan := RepositoryCard{
		Name: "order", URL: before.URL,
		AutoCard:    &AutoCard{Deps: []string{"fresh"}, ExposedAPIs: []string{"fastapi:/orders"}},
		ScanStatus:  ScanStatusOK,
		Fingerprint: "sha-2",
	}
	counts, err := RegisterScanned(ctx, store, []RepositoryCard{rescan})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Registered != 1 || counts.Skipped != 0 || counts.Failed != 0 {
		t.Fatalf("refresh counts = %+v, want 1 registered", counts)
	}
	stored, err := store.Get(ctx, before.ID)
	if err != nil || stored == nil {
		t.Fatalf("refreshed row missing: %v", err)
	}
	if stored.ID != before.ID {
		t.Fatal("refresh must keep the row identity")
	}
	if stored.AutoCard == nil || stored.AutoCard.Deps[0] != "fresh" {
		t.Fatal("refresh must replace the card")
	}
	if stored.Description != "operator typed this" || stored.TestCommands[0] != "pytest" {
		t.Fatal("refresh must not touch operator-owned fields")
	}
	if stored.Fingerprint != "sha-2" {
		t.Fatal("refresh must update the fingerprint")
	}
}

func TestRescanWithoutCardSkips(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	if _, err := RegisterScanned(ctx, store,
		[]RepositoryCard{cardProfile("order", "sha-1", &AutoCard{Deps: []string{"old"}})}); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetByName(ctx, "order")

	cardless := RepositoryCard{Name: "order", URL: before.URL, ScanStatus: ScanStatusOK}
	counts, err := RegisterScanned(ctx, store, []RepositoryCard{cardless})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Skipped != 1 || counts.Registered != 0 {
		t.Fatalf("counts = %+v, want skipped", counts)
	}
	after, _ := store.Get(ctx, before.ID)
	if after.AutoCard == nil || after.AutoCard.Deps[0] != "old" {
		t.Fatal("a cardless re-scan must not wipe the stored card")
	}
}

func TestFailedScanNeverRegistered(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	broken := cardProfile("broken", "", nil)
	broken.ScanStatus = ScanStatusFailed
	counts, err := RegisterScanned(ctx, store, []RepositoryCard{broken})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Failed != 1 || counts.Registered != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	if row, _ := store.GetByName(context.Background(), "broken"); row != nil {
		t.Fatal("a failed scan must not enter the catalog")
	}
}

func TestWriteFailureCountedNotRaised(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	store.failOn["bad"] = true
	counts, err := RegisterScanned(ctx, store, []RepositoryCard{
		cardProfile("good", "sha", &AutoCard{}),
		cardProfile("bad", "sha", &AutoCard{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Failed != 1 || counts.Registered != 1 {
		t.Fatalf("counts = %+v, want one failure isolated", counts)
	}
}

// ReplaceObservedCalls implements CatalogStore for the in-memory fake.
func (s *memoryStore) ReplaceObservedCalls(_ context.Context, id string, calls []ObservedCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, row := range s.rows {
		if row.ID == id {
			row.ObservedCalls = calls
			s.rows[name] = row
			return nil
		}
	}
	return nil
}
