package serverproc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// fakeServer stands in for multiversectl-server: it says where it serves, as
// the real one does, and waits to be stopped.
func fakeServer(t *testing.T, body string) *Process {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "multiversectl-server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Process{dir: filepath.Join(dir, "state"), binary: func() (string, error) { return bin, nil }}
}

func TestStartStatusStop(t *testing.T) {
	p := fakeServer(t, "trap 'exit 0' TERM\necho 'serving http://127.0.0.1:4242/?t=abc (it can act)'\nwhile :; do sleep 0.1; done\n")
	ctx := context.Background()

	if _, err := p.Status(ctx); !errors.Is(err, app.ErrServerNotRunning) {
		t.Fatalf("before start: %v", err)
	}
	info, err := p.Start(ctx, app.ServerStart{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Stop(ctx) })
	if info.URL != "http://127.0.0.1:4242/?t=abc" || info.PID == 0 {
		t.Fatalf("info = %+v", info)
	}

	again, err := p.Start(ctx, app.ServerStart{})
	if !errors.Is(err, app.ErrServerRunning) || again.PID != info.PID {
		t.Errorf("a second start must report the running one: %+v, %v", again, err)
	}
	if st, err := p.Status(ctx); err != nil || st.PID != info.PID {
		t.Errorf("status = %+v, %v", st, err)
	}

	stopped, err := p.Stop(ctx)
	if err != nil || stopped.PID != info.PID {
		t.Fatalf("stop = %+v, %v", stopped, err)
	}
	if _, err := p.Status(ctx); !errors.Is(err, app.ErrServerNotRunning) {
		t.Errorf("after stop: %v", err)
	}
	if _, err := p.Stop(ctx); !errors.Is(err, app.ErrServerNotRunning) {
		t.Errorf("stopping twice: %v", err)
	}
}

func TestStartReportsWhyItFailed(t *testing.T) {
	p := fakeServer(t, "echo 'listen tcp 127.0.0.1:7777: address already in use' >&2\nexit 1\n")
	_, err := p.Start(context.Background(), app.ServerStart{Addr: "127.0.0.1:7777"})
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("err = %v, want the server's own words", err)
	}
}

// A state file whose pid is not this program must never be signalled.
func TestStaleStateIsNotSignalled(t *testing.T) {
	p := fakeServer(t, "exit 0\n")
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := p.save(state{PID: os.Getpid(), URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stop(context.Background()); !errors.Is(err, app.ErrServerNotRunning) {
		t.Fatalf("this test process was taken for the server: %v", err)
	}
	if _, err := os.Stat(p.statePath()); err == nil {
		t.Error("the stale state file was left behind")
	}
}
