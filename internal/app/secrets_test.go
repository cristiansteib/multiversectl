package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// secretBench has an app "api" whose repository wants a .env, and universe 1.
func secretBench(t *testing.T) (*bench, domain.Universe) {
	t.Helper()
	b := newBench(t)
	b.svc.Manifest.Apps = map[string]domain.App{"api": {Repo: "api", Secrets: []string{".env"},
		// Declared, so creating the universe does not ask the runtime what the app answers on.
		Hosts: map[string]domain.HostRule{"api": {Service: "web", Port: 80}}}}
	if _, err := b.create(app.CreateInput{}); err != nil {
		t.Fatal(err)
	}
	return b, b.svc.Universe("1")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAMissingSecretStopsTheStartAndSaysWhereItGoes(t *testing.T) {
	b, u := secretBench(t)
	err := b.svc.PrepareSecrets(u, []string{"api"})
	if err == nil {
		t.Fatal("a secret that exists nowhere must stop the start")
	}
	for _, want := range []string{"api needs .env", filepath.Join(b.clone("api"), ".env"), "git ignores it", "run this again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestASecretPutInTheCloneAfterwardsIsPickedUpOnStart(t *testing.T) {
	b, u := secretBench(t)
	if err := os.WriteFile(filepath.Join(b.clone("api"), ".env"), []byte("TOKEN=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.PrepareSecrets(u, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(u.RepoDir("api"), ".env")); got != "TOKEN=mine\n" {
		t.Errorf("the universe's .env = %q", got)
	}
}

func TestAnExampleBesideTheSecretStartsItTheWayTheRepositoryDoes(t *testing.T) {
	b, u := secretBench(t)
	clone := b.clone("api")
	if err := os.WriteFile(filepath.Join(clone, ".env.example"), []byte("TOKEN=change-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.PrepareSecrets(u, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	// Made once in the shared clone, where the credentials are filled in, and
	// copied from there, so the next universe gets the same file.
	if got := readFile(t, filepath.Join(clone, ".env")); got != "TOKEN=change-me\n" {
		t.Errorf("the clone's .env = %q", got)
	}
	if got := readFile(t, filepath.Join(u.RepoDir("api"), ".env")); got != "TOKEN=change-me\n" {
		t.Errorf("the universe's .env = %q", got)
	}
	if !b.out.said("made from .env.example") || !b.out.said("put real credentials") {
		t.Errorf("it must say it used the example: %v", b.out.all)
	}

	// Filling it in is kept: the next start does not go back to the example.
	if err := os.WriteFile(filepath.Join(clone, ".env"), []byte("TOKEN=real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(u.RepoDir("api"), ".env")); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.PrepareSecrets(u, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(u.RepoDir("api"), ".env")); got != "TOKEN=real\n" {
		t.Errorf("after filling it in, the universe got %q", got)
	}
}

func TestASecretAlreadyInTheUniverseIsLeftAlone(t *testing.T) {
	b, u := secretBench(t)
	dst := filepath.Join(u.RepoDir("api"), ".env")
	if err := os.WriteFile(dst, []byte("TOKEN=edited-here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.PrepareSecrets(u, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst); got != "TOKEN=edited-here\n" {
		t.Errorf("an edit in the universe was overwritten: %q", got)
	}
}

func TestAManifestCanNameTheFileASecretIsMadeFrom(t *testing.T) {
	b, u := secretBench(t)
	b.svc.Manifest.Apps["api"] = withTemplates(b.svc.Manifest.Apps["api"], map[string]string{".env": "env.local"})
	clone := b.clone("api")
	// The convention is not looked at once the manifest has named another file.
	for name, body := range map[string]string{".env.example": "TOKEN=example\n", "env.local": "TOKEN=local\n"} {
		if err := os.WriteFile(filepath.Join(clone, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.svc.PrepareSecrets(u, []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(u.RepoDir("api"), ".env")); got != "TOKEN=local\n" {
		t.Errorf("the universe's .env = %q, want the one named", got)
	}
}

func TestAManifestCanSayASecretIsNeverMadeFromAnExample(t *testing.T) {
	b, u := secretBench(t)
	b.svc.Manifest.Apps["api"] = withTemplates(b.svc.Manifest.Apps["api"], map[string]string{".env": ""})
	if err := os.WriteFile(filepath.Join(b.clone("api"), ".env.example"), []byte("TOKEN=example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := b.svc.PrepareSecrets(u, []string{"api"})
	if err == nil || !strings.Contains(err.Error(), "never made from a template") {
		t.Fatalf("want the start stopped, saying why the example was not used, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(b.clone("api"), ".env")); statErr == nil {
		t.Error("a .env was made from the example although the manifest said not to")
	}
}

func withTemplates(a domain.App, t map[string]string) domain.App {
	a.SecretTemplates = t
	return a
}
