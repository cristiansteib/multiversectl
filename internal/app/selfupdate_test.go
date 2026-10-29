package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

// fakeReleases serves the named assets and publishes a checksums.txt over
// them, as the release pipeline does. tampered names are served with their
// bytes changed after the list was written, which is the attack the check is
// there for.
type fakeReleases struct {
	assets   map[string]string
	tampered map[string]bool
	noSums   bool
	omit     map[string]bool // left out of checksums.txt
	version  string          // the newest finished release; v2.0.0 when empty
	pending  string          // a newer version whose binaries are still being attached
}

func (f fakeReleases) Latest(context.Context) (app.Release, error) {
	urls := map[string]string{}
	for name := range f.assets {
		urls[name] = name
	}
	if !f.noSums {
		urls["checksums.txt"] = "checksums.txt"
	}
	version := f.version
	if version == "" {
		version = "v2.0.0"
	}
	return app.Release{Version: version, Assets: urls, Pending: f.pending}, nil
}

func (f fakeReleases) Recent(ctx context.Context) ([]app.Release, error) {
	rel, err := f.Latest(ctx)
	return []app.Release{rel}, err
}

func (f fakeReleases) Download(_ context.Context, url, dest string) (string, error) {
	body := f.assets[url]
	if url == "checksums.txt" {
		body = f.checksums()
	} else if f.tampered[url] {
		body += " (not what was published)"
	}
	if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]), nil
}

func (f fakeReleases) checksums() string {
	var names []string
	for name := range f.assets {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		if f.omit[name] {
			continue
		}
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256([]byte(f.assets[name])), name)
	}
	return b.String()
}

func update(t *testing.T, r fakeReleases, goos, arch string) (app.SelfUpdateResult, error, string, string) {
	t.Helper()
	dir := t.TempDir()
	cli, server := filepath.Join(dir, "multiversectl"), filepath.Join(dir, "multiversectl-server")
	svc := app.New(app.Deps{Files: fsx.New(), Releases: r}, dir, nil, "v1.0.0")
	res, err := svc.SelfUpdate(context.Background(), app.SelfUpdateInput{
		CurrentVersion: "v1.0.0", OS: goos, Arch: arch, BinaryPath: cli, ServerPath: server,
	})
	return res, err, cli, server
}

func TestSelfUpdateMovesTheWebViewWithTheCommandLine(t *testing.T) {
	res, err, cli, server := update(t, fakeReleases{assets: map[string]string{
		"multiversectl_darwin_arm64":        "cli 2",
		"multiversectl-server_darwin_arm64": "server 2",
	}}, "darwin", "arm64")
	if err != nil || !res.Updated || !res.Server {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for path, want := range map[string]string{cli: "cli 2", server: "server 2"} {
		if got, _ := os.ReadFile(path); string(got) != want {
			t.Errorf("%s = %q, want %q", filepath.Base(path), got, want)
		}
	}
}

func TestSelfUpdateToleratesAReleaseWithoutTheWebView(t *testing.T) {
	res, err, _, _ := update(t, fakeReleases{assets: map[string]string{
		"multiversectl_linux_amd64": "cli 2",
	}}, "linux", "amd64")
	if err != nil || !res.Updated || res.Server {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// The point of the exercise: bytes that are not the published ones are not
// run, and the binary that was there is still there.
func TestABinaryThatIsNotWhatTheReleaseSaysIsNotInstalled(t *testing.T) {
	res, err, cli, _ := update(t, fakeReleases{
		assets:   map[string]string{"multiversectl_linux_amd64": "cli 2"},
		tampered: map[string]bool{"multiversectl_linux_amd64": true},
	}, "linux", "amd64")
	if err == nil {
		t.Fatal("a tampered binary was installed")
	}
	if !strings.Contains(err.Error(), "checksum") || !strings.Contains(err.Error(), "nothing was installed") {
		t.Errorf("want it said plainly, got %q", err)
	}
	if res.Updated {
		t.Error("it reported an update it did not make")
	}
	if _, statErr := os.Stat(cli); statErr == nil {
		t.Error("the downloaded file was left where the binary goes")
	}
}

// The web view is installed after the command line, so a bad one leaves a
// mismatched pair. That is said, rather than reported as a clean update.
func TestAWebViewThatDoesNotMatchIsReportedAgainstTheCommandLineThatDid(t *testing.T) {
	res, err, _, server := update(t, fakeReleases{
		assets: map[string]string{
			"multiversectl_linux_amd64":        "cli 2",
			"multiversectl-server_linux_amd64": "server 2",
		},
		tampered: map[string]bool{"multiversectl-server_linux_amd64": true},
	}, "linux", "amd64")
	if err == nil {
		t.Fatal("a tampered web view was installed")
	}
	if !strings.Contains(err.Error(), "the web view is not") {
		t.Errorf("want the pair explained, got %q", err)
	}
	if !res.Updated || res.Server {
		t.Errorf("want the command line updated and the view not: %+v", res)
	}
	if _, statErr := os.Stat(server); statErr == nil {
		t.Error("the web view was replaced anyway")
	}
}

// "The integrity check was missing, so it was skipped" is the shape of the
// problem the check exists to prevent.
func TestAReleaseThatPublishesNoChecksumsIsNotInstalled(t *testing.T) {
	_, err, cli, _ := update(t, fakeReleases{
		assets: map[string]string{"multiversectl_linux_amd64": "cli 2"},
		noSums: true,
	}, "linux", "amd64")
	if err == nil {
		t.Fatal("it installed from a release it could not check")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("want the reason, got %q", err)
	}
	if _, statErr := os.Stat(cli); statErr == nil {
		t.Error("something was installed")
	}
}

func TestAnAssetMissingFromTheListIsNotInstalled(t *testing.T) {
	_, err, _, _ := update(t, fakeReleases{
		assets: map[string]string{"multiversectl_linux_amd64": "cli 2"},
		omit:   map[string]bool{"multiversectl_linux_amd64": true},
	}, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("want it refused for being unlisted, got %v", err)
	}
}

// A tag that is published before its binaries are attached is not "up to date":
// there is something newer, and nothing to download yet.
func TestSelfUpdateSaysWhenTheNewestVersionIsStillBeingPublished(t *testing.T) {
	res, err, _, _ := update(t, fakeReleases{version: "v1.0.0", pending: "v1.1.0", assets: map[string]string{
		"multiversectl_linux_amd64": "cli 1",
	}}, "linux", "amd64")
	if err != nil || res.Updated || res.Pending != "v1.1.0" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	res, err, _, _ = update(t, fakeReleases{version: "v1.0.0", assets: map[string]string{"multiversectl_linux_amd64": "cli 1"}}, "linux", "amd64")
	if err != nil || res.Updated || res.Pending != "" {
		t.Fatalf("nothing pending: res=%+v err=%v", res, err)
	}
}

func TestSelfUpdateNamesThePlatformAReleaseHasNoBinaryFor(t *testing.T) {
	_, err, _, _ := update(t, fakeReleases{assets: map[string]string{"multiversectl_linux_amd64": "cli 2"}}, "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "no binary for darwin/arm64 yet") || !strings.Contains(err.Error(), "try again shortly") {
		t.Fatalf("err = %v", err)
	}
}
