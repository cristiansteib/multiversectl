package system

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

type System struct{}

func New() *System { return &System{} }

func (System) UID() int                 { return os.Getuid() }
func (System) GID() int                 { return os.Getgid() }
func (System) Getenv(key string) string { return os.Getenv(key) }

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

// probeTimeout keeps a dead address from stalling doctor: it only has to tell
// "answers" from "does not".
const probeTimeout = 3 * time.Second

func (System) Probe(ctx context.Context, url string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}
