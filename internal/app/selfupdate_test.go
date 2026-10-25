package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

type fakeReleases struct{ assets map[string]string }

func (f fakeReleases) Latest(context.Context) (app.Release, error) {
	urls := map[string]string{}
	for name := range f.assets {
		urls[name] = name
	}
	return app.Release{Version: "v2.0.0", Assets: urls}, nil
}

func (f fakeReleases) Download(_ context.Context, url, dest string) error {
	return os.WriteFile(dest, []byte(f.assets[url]), 0o644)
}

func TestSelfUpdateMovesTheWebViewWithTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	cli, server := filepath.Join(dir, "multiversectl"), filepath.Join(dir, "multiversectl-server")
	svc := app.New(app.Deps{Files: fsx.New(), Releases: fakeReleases{assets: map[string]string{
		"multiversectl_darwin_arm64":        "cli 2",
		"multiversectl-server_darwin_arm64": "server 2",
	}}}, dir, nil, "v1.0.0")

	res, err := svc.SelfUpdate(context.Background(), app.SelfUpdateInput{
		CurrentVersion: "v1.0.0", OS: "darwin", Arch: "arm64", BinaryPath: cli, ServerPath: server,
	})
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
	dir := t.TempDir()
	svc := app.New(app.Deps{Files: fsx.New(), Releases: fakeReleases{assets: map[string]string{
		"multiversectl_linux_amd64": "cli 2",
	}}}, dir, nil, "v1.0.0")
	res, err := svc.SelfUpdate(context.Background(), app.SelfUpdateInput{
		CurrentVersion: "v1.0.0", OS: "linux", Arch: "amd64",
		BinaryPath: filepath.Join(dir, "multiversectl"), ServerPath: filepath.Join(dir, "multiversectl-server"),
	})
	if err != nil || !res.Updated || res.Server {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
