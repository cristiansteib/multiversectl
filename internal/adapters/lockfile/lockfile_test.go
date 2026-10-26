package lockfile_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/lockfile"
	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestOneAtATime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "project.lock")
	locks := lockfile.New()

	unlock, err := locks.Acquire(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// The directory is made on the way, since nothing else creates it.
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("the lock's folder was not made: %v", err)
	}
	unlock()

	// And it is free again afterwards.
	again, err := locks.Acquire(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("after releasing: %v", err)
	}
	again()
}

func TestReleasingTwiceIsHarmless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lockfile.New().Acquire(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock()
}

// The point of a lock file: the other process is a different program, and
// this is the only way to find out what it is doing. A goroutine here would
// prove nothing, because a lock within one process is a different question.
func TestAnotherProcessIsKeptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.lock")
	held := make(chan struct{})
	done := make(chan error, 1)

	// A child process that takes the lock and holds it until it is told.
	child := exec.Command(os.Args[0], "-test.run=TestHelperHolds", "-test.v")
	child.Env = append(os.Environ(), "MVCTL_LOCK_HELPER="+path)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); <-done }()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 && strings.Contains(string(buf[:n]), "HELD") {
				close(held)
				break
			}
			if err != nil {
				break
			}
		}
	}()
	go func() { done <- child.Wait() }()

	select {
	case <-held:
	case <-time.After(30 * time.Second):
		t.Fatal("the other process never said it had the lock")
	}

	start := time.Now()
	_, err = lockfile.New().Acquire(context.Background(), path, 200*time.Millisecond)
	var busy *app.Busy
	if !errors.As(err, &busy) {
		t.Fatalf("want it refused while another process holds it, got %v", err)
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("it gave up after %s without waiting out its patience", waited)
	}
	// And it can say who to wait for, which is the whole reason for the file.
	if !strings.Contains(busy.Holder, "pid ") {
		t.Errorf("want the holder named, got %q", busy.Holder)
	}
}

// TestHelperHolds is the child of the test above, not a test of its own.
func TestHelperHolds(t *testing.T) {
	path := os.Getenv("MVCTL_LOCK_HELPER")
	if path == "" {
		t.Skip("run as a child of TestAnotherProcessIsKeptOut")
	}
	unlock, err := lockfile.New().Acquire(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("the child could not take the lock: %v", err)
	}
	defer unlock()
	os.Stdout.WriteString("HELD\n")
	// Until the parent closes stdin, or gives up on us.
	io := make([]byte, 1)
	os.Stdin.Read(io)
}

func TestGivingUpWhenTheCallerDoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lockfile.New().Acquire(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lockfile.New().Acquire(ctx, path, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("want the caller's cancellation, got %v", err)
	}
}
