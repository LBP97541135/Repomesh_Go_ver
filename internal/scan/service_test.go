package scan

import "testing"

type fakeChannel struct{ name string }

func (f fakeChannel) Name() string                                 { return f.name }
func (f fakeChannel) Select(tree []FileEntry) []string             { return nil }
func (f fakeChannel) Parse(filename, content string) ChannelOutput { return ChannelOutput{} }

type fakeStore struct{ CatalogStore }

func TestNewAppliesDefaults(t *testing.T) {
	s := New(Config{}, fakeStore{})
	if s.MaxWorkers() != 5 {
		t.Fatalf("MaxWorkers default = %d, want 5", s.MaxWorkers())
	}
	if s.ScopeAssistEnabled() {
		t.Fatal("scope assist must default to off (manual selection is the core path)")
	}
	if len(s.Channels()) != 0 {
		t.Fatalf("fresh service has %d channels, want 0", len(s.Channels()))
	}
}

func TestRegisterChannelPreservesScanOrder(t *testing.T) {
	s := New(Config{ScopeAssistEnabled: true, MaxWorkers: 3}, fakeStore{})
	s.RegisterChannel(fakeChannel{name: "build"})
	s.RegisterChannel(fakeChannel{name: "runtime_call"})

	got := s.Channels()
	if len(got) != 2 || got[0].Name() != "build" || got[1].Name() != "runtime_call" {
		t.Fatalf("channel order = %v, want [build runtime_call]", names(got))
	}
	if !s.ScopeAssistEnabled() || s.MaxWorkers() != 3 {
		t.Fatal("config not assembled as given")
	}
}

func names(channels []EvidenceChannel) []string {
	out := make([]string, len(channels))
	for i, ch := range channels {
		out[i] = ch.Name()
	}
	return out
}
