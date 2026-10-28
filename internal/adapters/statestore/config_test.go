package statestore

import (
	"errors"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// The manifest a universe was started with is kept beside its state, and goes with it.
func TestTheManifestAUniverseWasStartedWithIsKeptBesideItsState(t *testing.T) {
	s := New(t.TempDir())
	st := domain.UniverseState{Name: "1", ID: "1", Slug: "1"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadConfig("1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("nothing kept yet: %v", err)
	}
	if err := s.SaveConfig("1", []byte("name: acme\n")); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig("1")
	if err != nil || string(got) != "name: acme\n" {
		t.Fatalf("LoadConfig = %q %v", got, err)
	}
	if err := s.Delete("1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadConfig("1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("it goes with the universe: %v", err)
	}
}

// What was recorded survives the file it is kept in.
func TestTheRecordOfTheStartRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	st := domain.UniverseState{Name: "1", ID: "1", Slug: "1", StartedWith: domain.ConfigRecord{SHA256: "abc", Version: "v9"}}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("1")
	if err != nil || got.StartedWith.SHA256 != "abc" || got.StartedWith.Version != "v9" {
		t.Fatalf("loaded %+v %v", got.StartedWith, err)
	}
}
