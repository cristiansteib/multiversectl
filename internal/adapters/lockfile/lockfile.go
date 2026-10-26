// Package lockfile keeps two processes out of each other's way with a lock
// file per thing being worked on.
//
// It uses flock rather than a file whose existence means "taken": a lock held
// by a process is released by the kernel when that process ends, however it
// ends, so a crash or a kill leaves nothing to clean up by hand. The file
// itself is only somewhere to hang the lock, and holds the name of whoever
// took it so the next one can be told who to wait for.
package lockfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// poll is how often a waiting process tries again. flock can block instead,
// but then nothing could give up when the caller does.
const poll = 50 * time.Millisecond

type Locks struct{}

var _ app.Locks = Locks{}

func New() Locks { return Locks{} }

func (Locks) Acquire(ctx context.Context, path string, wait time.Duration) (app.Unlock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			// Whoever waits next reads this to say who they are waiting for.
			// A failure to write it costs the message, not the lock.
			_ = f.Truncate(0)
			_, _ = f.WriteAt([]byte(describe()), 0)
			var once sync.Once
			return func() {
				once.Do(func() {
					_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
					_ = f.Close()
				})
			}, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			f.Close()
			return nil, ctxErr
		}
		if time.Now().After(deadline) {
			holder := readHolder(f)
			f.Close()
			return nil, &app.Busy{Holder: holder}
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// describe is how this process names itself to whoever waits for it.
func describe() string {
	who := filepath.Base(os.Args[0])
	if len(os.Args) > 1 {
		who += " " + strings.Join(os.Args[1:], " ")
	}
	if len(who) > 120 {
		who = who[:120]
	}
	return fmt.Sprintf("pid %d: %s\n", os.Getpid(), who)
}

// readHolder is who wrote the file, for the message. It is a courtesy: an
// unreadable or empty file means the holder goes unnamed, not that the lock
// is free.
func readHolder(f *os.File) string {
	buf := make([]byte, 256)
	n, _ := f.ReadAt(buf, 0)
	return strings.TrimSpace(string(buf[:n]))
}
