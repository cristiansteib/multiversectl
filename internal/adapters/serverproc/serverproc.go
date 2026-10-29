// Package serverproc starts, finds and stops the multiversectl-server process.
package serverproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
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
	base, err := userdirs.Config()
	if err != nil {
		return nil, fmt.Errorf("locate config directory: %w", err)
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
			// A port of 0 was the system's to choose, and a busy one is replaced by the
			// next free: a restart keeps the port actually in use, on the host asked for.
			if u, err := url.Parse(st.URL); err == nil && u.Port() != "" {
				if host, _, err := net.SplitHostPort(o.Addr); err == nil {
					st.Addr = net.JoinHostPort(host, u.Port())
				}
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

// Pair asks the running server for a code. The state file holds the link it
// was started with, and the token in that link is the session cookie itself,
// so this is the same request the page would make.
func (p *Process) Pair(ctx context.Context) (app.ServerInvite, error) {
	info, err := p.Status(ctx)
	if err != nil {
		return app.ServerInvite{}, err
	}
	u, err := url.Parse(info.URL)
	if err != nil {
		return app.ServerInvite{}, fmt.Errorf("the recorded address %q cannot be read: %w", info.URL, err)
	}
	token := u.Query().Get("t")
	if token == "" {
		return app.ServerInvite{}, errors.New("the recorded address carries no token; restart the server")
	}
	// Whatever address it was started on, this process is on the same machine.
	ask := &url.URL{Scheme: "http", Host: localTo(u.Host), Path: "/api/pair"}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ask.String(), nil)
	if err != nil {
		return app.ServerInvite{}, err
	}
	req.Header.Set("X-Mvctl", "1")
	req.AddCookie(&http.Cookie{Name: "mvctl", Value: token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return app.ServerInvite{}, fmt.Errorf("ask the server for a code: %w", err)
	}
	defer res.Body.Close()
	var body struct {
		Code    string   `json:"code"`
		Expires string   `json:"expires"`
		At      []string `json:"at"`
		Exposed bool     `json:"exposed"`
		Error   string   `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body); err != nil {
		return app.ServerInvite{}, fmt.Errorf("the server answered with something unexpected: %w", err)
	}
	if res.StatusCode == http.StatusNotFound {
		return app.ServerInvite{}, errors.New("the running server is an older build, from before codes: `multiversectl server restart`")
	}
	if res.StatusCode != http.StatusOK {
		if body.Error == "" {
			body.Error = res.Status
		}
		return app.ServerInvite{}, errors.New(body.Error)
	}
	expires, _ := time.Parse(time.RFC3339, body.Expires)
	return app.ServerInvite{Code: body.Code, Expires: expires, At: body.At, Exposed: body.Exposed}, nil
}

// localTo turns the address the server reports into one this machine can
// reach it on: a server listening on every interface answers on the loopback
// address too, and that is the one that is certain to work from here.
func localTo(host string) string {
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return host
}
