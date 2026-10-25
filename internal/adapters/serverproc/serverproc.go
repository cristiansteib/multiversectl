// Package serverproc starts, finds and stops the multiversectl-server process.
package serverproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

const binaryName = "multiversectl-server"

// Process keeps what it needs to find the server again in the config
// directory: a state file with the pid and the address, and the server's log.
type Process struct {
	dir    string
	binary func() (string, error)
}

var _ app.ServerProcess = (*Process)(nil)

// New keeps its files under $XDG_CONFIG_HOME/multiversectl, beside the config.
func New() (*Process, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate config directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return &Process{dir: filepath.Join(base, "multiversectl"), binary: locate}, nil
}

type state struct {
	PID     int       `json:"pid"`
	URL     string    `json:"url"`
	Started time.Time `json:"started"`
	// What it was started with, so a restart is the same server, and which
	// program that was, so a later update of the file can be told from it.
	Addr      string `json:"addr,omitempty"`
	Expose    bool   `json:"expose,omitempty"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
	Binary    string `json:"binary,omitempty"`
	BinaryMod int64  `json:"binaryMod,omitempty"`
}

func (p *Process) statePath() string { return filepath.Join(p.dir, "server.json") }
func (p *Process) logPath() string   { return filepath.Join(p.dir, "server.log") }

// locate looks beside this executable first, which is where both binaries are
// installed together, then on the PATH.
func locate() (string, error) {
	if self, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(self); err == nil {
			self = real
		}
		next := filepath.Join(filepath.Dir(self), binaryName)
		if info, err := os.Stat(next); err == nil && !info.IsDir() {
			return next, nil
		}
	}
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s is not installed: it is a separate program, installed beside multiversectl by `make install` and published with each release", binaryName)
}

var served = regexp.MustCompile(`serving (http://\S+)`)

func (p *Process) Start(ctx context.Context, o app.ServerStart) (app.ServerInfo, error) {
	if info, err := p.Status(ctx); err == nil {
		return info, app.ErrServerRunning
	}
	bin, err := p.binary()
	if err != nil {
		return app.ServerInfo{}, err
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return app.ServerInfo{}, err
	}
	log, err := os.OpenFile(p.logPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return app.ServerInfo{}, err
	}
	defer log.Close()

	args := []string{"--addr", o.Addr}
	if o.Expose {
		args = append(args, "--expose")
	}
	if o.ReadOnly {
		args = append(args, "--read-only")
	}
	// Not CommandContext: the server must outlive this command. Its own session
	// keeps the shell's Ctrl+C and hangup from reaching it.
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return app.ServerInfo{}, fmt.Errorf("start %s: %w", bin, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }() // also reaps it, so it never lingers as a zombie

	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return app.ServerInfo{}, ctx.Err()
		case err := <-exited:
			return app.ServerInfo{}, fmt.Errorf("the server exited at once (%v):\n%s", err, p.tail())
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return app.ServerInfo{}, fmt.Errorf("the server did not report an address in time:\n%s", p.tail())
		case <-tick.C:
			data, _ := os.ReadFile(p.logPath())
			m := served.FindSubmatch(data)
			if m == nil {
				continue
			}
			st := state{PID: cmd.Process.Pid, URL: string(m[1]), Started: time.Now(),
				Addr: o.Addr, Expose: o.Expose, ReadOnly: o.ReadOnly, Binary: bin, BinaryMod: modTime(bin)}
			// A port of 0 was the system's to choose; a restart keeps the one it chose.
			if u, err := url.Parse(st.URL); err == nil && strings.HasSuffix(o.Addr, ":0") && u.Host != "" {
				st.Addr = u.Host
			}
			if err := p.save(st); err != nil {
				_ = cmd.Process.Kill()
				return app.ServerInfo{}, err
			}
			return p.info(st), nil
		}
	}
}

func (p *Process) Status(context.Context) (app.ServerInfo, error) {
	st, err := p.load()
	if err != nil {
		return app.ServerInfo{}, app.ErrServerNotRunning
	}
	if !ours(st.PID) {
		// A stale file: the process ended, or its pid belongs to something else now.
		_ = os.Remove(p.statePath())
		return app.ServerInfo{}, app.ErrServerNotRunning
	}
	return p.info(st), nil
}

func (p *Process) Stop(ctx context.Context) (app.ServerInfo, error) {
	info, err := p.Status(ctx)
	if err != nil {
		return app.ServerInfo{}, err
	}
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		return info, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return info, fmt.Errorf("stop pid %d: %w", info.PID, err)
	}
	// The server shuts down gracefully on SIGTERM; one that does not within
	// the wait is killed rather than left holding the port.
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if !ours(info.PID) {
			_ = os.Remove(p.statePath())
			return info, nil
		}
	}
	_ = proc.Kill()
	_ = os.Remove(p.statePath())
	return info, nil
}

func (p *Process) info(st state) app.ServerInfo {
	outdated := st.Binary != "" && modTime(st.Binary) != st.BinaryMod
	return app.ServerInfo{PID: st.PID, URL: st.URL, Started: st.Started, Log: p.logPath(), Outdated: outdated}
}

// modTime is when the file was last replaced, zero when it cannot be read. An
// update writes a new file and renames it over the old, which moves this.
func modTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// Restart stops the running server and starts it with what it was started with.
func (p *Process) Restart(ctx context.Context) (app.ServerInfo, error) {
	st, err := p.load()
	if err != nil || !ours(st.PID) {
		return app.ServerInfo{}, app.ErrServerNotRunning
	}
	if _, err := p.Stop(ctx); err != nil {
		return app.ServerInfo{}, err
	}
	addr := st.Addr
	if addr == "" {
		addr = "127.0.0.1:7777" // a state file from before the options were kept
	}
	return p.Start(ctx, app.ServerStart{Addr: addr, Expose: st.Expose, ReadOnly: st.ReadOnly})
}

func (p *Process) load() (state, error) {
	var st state
	data, err := os.ReadFile(p.statePath())
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

func (p *Process) save(st state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(p.statePath(), data, 0o600)
}

// tail is the end of the server's log, which is where it says why it failed.
func (p *Process) tail() string {
	data, err := os.ReadFile(p.logPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return "  " + strings.Join(lines, "\n  ")
}

// ours says whether pid is a live multiversectl-server. A pid alone is not
// enough: after a reboot the number may be anyone's, and it is signalled.
func ours(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return strings.Contains(string(data), binaryName)
	}
	out, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "command=").Output()
	return err == nil && strings.Contains(string(out), binaryName)
}
