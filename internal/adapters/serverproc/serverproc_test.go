package serverproc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

const serving = "trap 'exit 0' TERM\necho 'serving http://127.0.0.1:4242/?t=abc (it can act)'\nwhile :; do sleep 0.1; done\n"

func TestRestartStartsItAgainWithTheOptionsItHad(t *testing.T) {
	// The fake echoes its arguments into the log, which is how the test sees them.
	p := fakeServer(t, "trap 'exit 0' TERM\necho \"args: $*\"\necho 'serving http://127.0.0.1:9000/?t=abc'\nwhile :; do sleep 0.1; done\n")
	ctx := context.Background()
	if _, err := p.Restart(ctx); !errors.Is(err, app.ErrServerNotRunning) {
		t.Fatalf("restarting what is not running: %v", err)
	}
	first, err := p.Start(ctx, app.ServerStart{Addr: "127.0.0.1:9000", Expose: true, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Stop(ctx) })

	second, err := p.Restart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.PID == first.PID || second.PID == 0 {
		t.Errorf("a restart is a new process: %d then %d", first.PID, second.PID)
	}
	if ours(first.PID) {
		t.Error("the old process is still running")
	}
	log, _ := os.ReadFile(p.logPath())
	if !strings.Contains(string(log), "args: --addr 127.0.0.1:9000 --expose --read-only") {
		t.Errorf("the restart did not keep the options: %q", log)
	}
}

func TestARestartKeepsThePortTheSystemChose(t *testing.T) {
	p := fakeServer(t, "trap 'exit 0' TERM\necho \"args: $*\"\necho 'serving http://127.0.0.1:4242/?t=abc'\nwhile :; do sleep 0.1; done\n")
	ctx := context.Background()
	if _, err := p.Start(ctx, app.ServerStart{Addr: "127.0.0.1:0"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Stop(ctx) })
	if _, err := p.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(p.logPath())
	if !strings.Contains(string(log), "args: --addr 127.0.0.1:4242") {
		t.Errorf("a restart must not pick another port: %q", log)
	}
}

func TestAServerOlderThanTheInstalledProgramIsSaidSo(t *testing.T) {
	p := fakeServer(t, serving)
	ctx := context.Background()
	info, err := p.Start(ctx, app.ServerStart{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Stop(ctx) })
	if info.Outdated {
		t.Fatal("just started, it is the installed build")
	}

	// An update writes a new file over the old one.
	bin, _ := p.binary()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(bin, later, later); err != nil {
		t.Fatal(err)
	}
	st, err := p.Status(ctx)
	if err != nil || !st.Outdated {
		t.Errorf("after the program was replaced: %+v, %v", st, err)
	}
	restarted, err := p.Restart(ctx)
	if err != nil || restarted.Outdated {
		t.Errorf("a restart takes the new build: %+v, %v", restarted, err)
	}
}

func TestABusyPortTheServerMovedFromIsNotAskedForAgain(t *testing.T) {
	p := fakeServer(t, "trap 'exit 0' TERM\necho \"args: $*\"\necho 'serving http://127.0.0.1:9001/?t=abc'\nwhile :; do sleep 0.1; done\n")
	ctx := context.Background()
	if _, err := p.Start(ctx, app.ServerStart{Addr: "127.0.0.1:9000"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Stop(ctx) })
	if _, err := p.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(p.logPath())
	if !strings.Contains(string(log), "args: --addr 127.0.0.1:9001") {
		t.Errorf("the restart asked for the busy port again: %q", log)
	}
}
