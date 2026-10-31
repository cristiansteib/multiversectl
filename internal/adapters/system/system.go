package system

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type System struct{}

func New() *System { return &System{} }

func (System) UID() int { return os.Getuid() }
func (System) GID() int { return os.Getgid() }

// Getenv is the environment, except that with a state directory the places
// the user's files go (HOME and the XDG directories) are inside it: the shell
// completion, the agent skill and "~/" in a path are written under it too.
func (System) Getenv(key string) string {
	if userdirs.Dir() != "" {
		var dir string
		var err error
		switch key {
		// ZDOTDIR is where zsh reads .zshrc: under a state directory it is the
		// relocated home, so a rehearsal never edits the real startup file.
		case "HOME", "ZDOTDIR":
			dir, err = userdirs.Home()
		case "XDG_CONFIG_HOME":
			dir, err = userdirs.Config()
		case "XDG_DATA_HOME":
			dir, err = userdirs.Data()
		case "XDG_STATE_HOME":
			dir, err = userdirs.State()
		case "XDG_CACHE_HOME":
			dir, err = userdirs.Cache()
		default:
			return os.Getenv(key)
		}
		if err == nil {
			return dir
		}
	}
	return os.Getenv(key)
}

func (System) ProcessEnv(key string) string { return os.Getenv(key) }

func (System) Which(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

func (System) LANIP(ctx context.Context) (string, error) {
	// A UDP "connection" sends nothing; it only makes the kernel pick the source address it would route from.
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", "1.1.1.1:80")
	if err != nil {
		return "", fmt.Errorf("cannot determine LAN IP: %w", err)
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil || addr.IP.IsLoopback() || addr.IP.IsUnspecified() {
		return "", errors.New("cannot determine LAN IP: no routable interface")
	}
	return addr.IP.String(), nil
}

func (System) Resolve(ctx context.Context, host string) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, nil
		}
		return nil, err
	}
	return addrs, nil
}

func (System) PortFree(ctx context.Context, port int) (bool, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil {
		// Not being allowed to bind says nothing about whether the port is in
		// use: a privileged port is refused to us while the container runtime,
		// running as root, binds it happily. Reporting that as a conflict sends
		// people hunting for a process that does not exist.
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return true, nil
		}
		return false, nil
	}
	return true, l.Close()
}

// FreePort lets the kernel choose: it knows every port in use, and an address
// nobody hard-coded cannot clash with one somebody did.
func (System) FreePort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "0.0.0.0:0")
	if err != nil {
		return 0, fmt.Errorf("finding a free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// probeTimeout keeps a dead address from stalling status: it only has to tell
// "answers" from "does not".
const probeTimeout = 3 * time.Second

func (System) Probe(ctx context.Context, url string) (app.Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return app.Answer{}, err
	}
	// The address is the proxy's own, and on a TLS entrypoint with no certificate
	// configured it presents a self-signed one: what is asked is whether the app
	// answers, not whether a browser would trust it. Each probe is one request
	// on a transport of its own, so no connection is kept alive after it: `up`
	// polls every two seconds, and the web view's server runs for days.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see above
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return app.Answer{}, err
	}
	defer resp.Body.Close()
	// Only the start: enough to recognise an error page, not to hold a download.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return app.Answer{Status: resp.StatusCode, Body: string(body)}, nil
}

// RunHost runs a command with no shell, in dir, adding env to this process's own.
func (System) RunHost(ctx context.Context, c app.HostCommand) (string, error) {
	if len(c.Args) == 0 {
		return "", errors.New("no command")
	}
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = os.Environ()
	for name, value := range c.Env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// Dial connects to address and closes the connection: whether something listens.
func (System) Dial(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

// FreeSpace is the bytes available to this user on the filesystem holding path.
func (System) FreeSpace(_ context.Context, path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:gosec // both are non-negative
}
