package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// What the report needs from the outside, in memory.
type cfgStore struct {
	states map[string]domain.UniverseState
	kept   map[string][]byte
}

func (c *cfgStore) Save(st domain.UniverseState) error { c.states[st.ID] = st; return nil }
func (c *cfgStore) Load(id string) (domain.UniverseState, error) {
	if st, ok := c.states[id]; ok {
		return st, nil
	}
	return domain.UniverseState{}, domain.ErrNotFound
}
func (c *cfgStore) List() ([]domain.UniverseState, error) {
	var out []domain.UniverseState
	for _, st := range c.states {
		out = append(out, st)
	}
	return out, nil
}
func (c *cfgStore) Delete(string) error { return nil }
func (c *cfgStore) SaveConfig(id string, data []byte) error {
	c.kept[id] = data
	return nil
}
func (c *cfgStore) LoadConfig(id string) ([]byte, error) {
	if d, ok := c.kept[id]; ok {
		return d, nil
	}
	return nil, domain.ErrNotFound
}
func (c *cfgStore) Notes(string) ([]domain.Note, error)  { return nil, nil }
func (c *cfgStore) CreateNote(string, domain.Note) error { return nil }
func (c *cfgStore) SaveNote(string, domain.Note) error   { return nil }

type cfgFiles struct {
	Files
	text string
}

func (f cfgFiles) ReadFile(string) ([]byte, error) { return []byte(f.text), nil }

// cfgLoader finds a manifest wrong when its text says BAD, with every problem listed.
type cfgLoader struct{ ManifestLoader }

func (cfgLoader) Decode(data []byte, _ string) (*domain.Manifest, error) {
	if strings.Contains(string(data), "BAD") {
		return nil, &domain.ValidationError{Problems: []string{"domain: required", "repos: at least one is required"}}
	}
	if strings.Contains(string(data), "BROKEN") {
		return nil, errors.New("manifest is not valid YAML")
	}
	return &domain.Manifest{}, nil
}

func cfgService(current string, st domain.UniverseState, kept ...string) (*Service, *cfgStore) {
	store := &cfgStore{states: map[string]domain.UniverseState{st.ID: st}, kept: map[string][]byte{}}
	if len(kept) > 0 {
		store.kept[st.ID] = []byte(kept[0])
	}
	s := New(Deps{Store: store, Files: cfgFiles{text: current}, Manifests: cfgLoader{}, Out: quietOut{}}, "acme", &domain.Manifest{Path: "/m/multiverse.yaml"}, "v1")
	return s, store
}

func universe(started string) domain.UniverseState {
	st := domain.UniverseState{Name: "1", ID: "1", Slug: "1", Number: 1}
	if started != "" {
		st.StartedWith = domain.ConfigRecord{SHA256: ConfigSHA([]byte(started)), At: time.Unix(1700000000, 0).UTC(), Version: "v0"}
	}
	return st
}

func TestTheConfigOfAUniverseIsReportedAgainstWhatItWasStartedWith(t *testing.T) {
	const then = "name: acme\n"
	// The same: not changed.
	s, _ := cfgService(then, universe(then), then)
	rep, err := s.UniverseConfig("1")
	if err != nil || rep.Changed || rep.StartedWith == nil || string(rep.Snapshot) != then || len(rep.Problems) != 0 {
		t.Fatalf("same: %+v %v", rep, err)
	}
	// Edited since: changed, and both texts are there to compare.
	s, _ = cfgService("name: acme\ndomain: example.test\n", universe(then), then)
	rep, _ = s.UniverseConfig("1")
	if !rep.Changed || string(rep.Snapshot) != then || !strings.Contains(string(rep.Current), "example.test") || rep.SHA256 == rep.StartedWith.SHA256 {
		t.Fatalf("edited: %+v", rep)
	}
	// Never started since this was kept: nothing to compare with, and not "changed".
	s, _ = cfgService(then, universe(""))
	rep, _ = s.UniverseConfig("1")
	if rep.Changed || rep.StartedWith != nil || rep.Snapshot != nil {
		t.Fatalf("not recorded: %+v", rep)
	}
	// Started, but the text was not kept (a copy that went missing): the record stands.
	s, _ = cfgService("name: acme\n# edited\n", universe(then))
	rep, err = s.UniverseConfig("1")
	if err != nil || !rep.Changed || rep.Snapshot != nil {
		t.Fatalf("no copy: %+v %v", rep, err)
	}
}

// The same checks that load a manifest are run on the text, on both.
func TestTheConfigIsCheckedAsLoadingItIs(t *testing.T) {
	s, _ := cfgService("BAD current", universe("BROKEN then"), "BROKEN then")
	rep, _ := s.UniverseConfig("1")
	if len(rep.Problems) != 2 || rep.Problems[0] != "domain: required" {
		t.Errorf("problems = %v", rep.Problems)
	}
	if len(rep.SnapshotProblems) != 1 || !strings.Contains(rep.SnapshotProblems[0], "not valid YAML") {
		t.Errorf("snapshot problems = %v", rep.SnapshotProblems)
	}
}

func TestStartingKeepsTheManifestItStartsWith(t *testing.T) {
	s, store := cfgService("name: acme\n", universe(""))
	rec, err := s.recordConfig("1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.SHA256 != ConfigSHA([]byte("name: acme\n")) || rec.Version != "v1" || rec.At.IsZero() {
		t.Fatalf("record = %+v", rec)
	}
	if string(store.kept["1"]) != "name: acme\n" {
		t.Fatalf("kept = %q", store.kept["1"])
	}
}
