package traefik

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/cristiansteib/multiversectl/internal/adapters/trace"
	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// proxyProject is the shared proxy's compose project. Relocating the state
// directory gives it a name of its own, so a rehearsal cannot recreate the
// proxy that serves the universes the machine really has.
func proxyProject() string { return "multiverse-proxy" + userdirs.Suffix() }

//go:embed templates/*.tmpl
var templates embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	// A JSON string is a valid double-quoted YAML scalar; backticks pass through.
	"q": func(s string) string { b, _ := json.Marshal(s); return string(b) },
}).ParseFS(templates, "templates/*.tmpl"))

// Traefik is the ingress: it runs the proxy container (app.Proxy) and
// expresses routes as compose labels (app.Router).
type Traefik struct {
	stdout, stderr io.Writer
	trace          *trace.Tracer
}

var (
	_ app.Proxy  = (*Traefik)(nil)
	_ app.Router = (*Traefik)(nil)
)

// New streams child output to the given writers; nil means os.Stdout / os.Stderr.
// The tracer, which may be nil, says which commands it runs.
func New(stdout, stderr io.Writer, t *trace.Tracer) *Traefik {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Traefik{stdout: stdout, stderr: stderr, trace: t}
}

func (t *Traefik) EnsureNetwork(ctx context.Context, name string) error {
	inspect := exec.CommandContext(ctx, "docker", "network", "inspect", name)
	done := t.trace.Begin(inspect)
	err := inspect.Run()
	done(err)
	if err == nil {
		return nil
	}
	return t.docker(ctx, "network", "create", name)
}

// servedByLabel names, on a routed container, the proxy that serves it. A proxy that is
// created again joins the networks of the containers that name it, and of no others:
// another proxy (a rehearsal under its own state directory) serves its own.
const servedByLabel = "io.multiversectl.served-by"

// capture runs docker and returns what it printed. Unlike docker(), it does not
// stream: these are the quiet bookkeeping commands around the proxy's networks.
func (t *Traefik) capture(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	done := t.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		return out.String(), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// proxyContainer is the id of the running proxy, empty when it is not running.
func (t *Traefik) proxyContainer(ctx context.Context) (string, error) {
	out, err := t.capture(ctx, "ps", "--quiet",
		"--filter", "label=com.docker.compose.project="+proxyProject(),
		"--filter", "label=com.docker.compose.service=traefik")
	if err != nil {
		return "", err
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// ConnectNetwork joins the proxy to a network; being on it already is fine. The
// proxy keeps running: a connection is made to the container, not to its compose file.
func (t *Traefik) ConnectNetwork(ctx context.Context, name string) error {
	id, err := t.proxyContainer(ctx)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("the proxy is not running, so it cannot be connected to network %s", name)
	}
	if _, err := t.capture(ctx, "network", "connect", name, id); err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// DisconnectProject takes the proxy off every network of a compose project, which
// has to happen before the project is taken down: compose cannot remove a network
// something is still connected to. The networks are found by compose's own label
// rather than by an assumed name, so a compose file that names its own networks is
// covered too. A proxy that is not running, or not on them, has nothing to leave.
func (t *Traefik) DisconnectProject(ctx context.Context, project string) error {
	id, err := t.proxyContainer(ctx)
	if err != nil || id == "" {
		return nil
	}
	out, err := t.capture(ctx, "network", "ls", "--quiet", "--filter", "label=com.docker.compose.project="+project)
	if err != nil {
		return err
	}
	for _, network := range strings.Fields(out) {
		if _, err := t.capture(ctx, "network", "disconnect", network, id); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "is not connected") || strings.Contains(msg, "not found") || strings.Contains(msg, "No such network") {
				continue
			}
			return err
		}
	}
	return nil
}

// reconnect joins a proxy that was just created to the networks its routes are on:
// its connections are not part of its compose file, so a new container has none.
// The networks are the ones the routed containers name in their traefik.docker.network label.
func (t *Traefik) reconnect(ctx context.Context) error {
	out, err := t.capture(ctx, "ps", "--all", "--filter", "label="+servedByLabel+"="+proxyProject(), "--format", `{{.Label "traefik.docker.network"}}`)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, name := range strings.Fields(out) {
		if seen[name] {
			continue
		}
		seen[name] = true
		// A network that is gone (its app was taken down) has nothing to reach.
		if err := t.ConnectNetwork(ctx, name); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "No such network") {
			return err
		}
	}
	return nil
}

func (t *Traefik) EnsureUp(ctx context.Context, r app.ProxyRequest) error {
	file, err := proxyFile()
	if err != nil {
		return err
	}
	eps := entrypointsOf(r.Listeners)
	certs, err := mountCertificates(r.Certificates)
	if err != nil {
		return err
	}
	data := proxyData{Network: r.Network, Socket: dockerSocket(), Entrypoints: eps,
		Certificates: certs, TLSFile: filepath.Join(filepath.Dir(file), "tls.yml")}
	if len(certs) > 0 {
		tls, err := render("tls.yml.tmpl", data)
		if err != nil {
			return err
		}
		if err := writeIfChanged(data.TLSFile, tls); err != nil {
			return err
		}
	}
	content, err := render("proxy.yml.tmpl", data)
	if err != nil {
		return err
	}
	if err := writeIfChanged(file, content); err != nil {
		return err
	}
	if err := t.docker(ctx, "compose", "--project-name", proxyProject(), "-f", file, "up", "-d"); err != nil {
		return err
	}
	return t.reconnect(ctx)
}

func (t *Traefik) Down(ctx context.Context) error {
	file, err := proxyFile()
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	return t.docker(ctx, "compose", "--project-name", proxyProject(), "-f", file, "down")
}

func (t *Traefik) Status(ctx context.Context) (app.ProxyStatus, error) {
	var st app.ProxyStatus
	file, err := proxyFile()
	if err != nil {
		return st, err
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return st, nil
	}
	st.Listeners = parseListeners(string(content))
	st.Certificates = parseCertificatePairs(string(content))

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "compose", "--project-name", proxyProject(), "-f", file, "ps", "--format", "{{json .}}")
	cmd.Stderr = &stderr
	done := t.trace.Begin(cmd)
	out, err := cmd.Output()
	done(err)
	if err != nil {
		return st, fmt.Errorf("docker compose ps: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	st.Running = anyRunning(out)
	return st, nil
}

func (t *Traefik) Materialize(ctx context.Context, r app.OverrideRequest) ([]string, error) {
	if len(r.Routes) == 0 && len(r.Unpublish) == 0 && len(r.Env) == 0 {
		return nil, nil
	}
	content, err := render("routes.yml.tmpl", buildOverride(r))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(r.Dir, ".multiverse-routes-"+r.App+".yml")
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

type entrypoint struct {
	Name string
	Port int
	Bind string // the host address it is published on; each entrypoint has its own
	TLS  bool
}

type proxyData struct {
	Network      string
	Socket       string
	Entrypoints  []entrypoint
	Certificates []mountedCert
	TLSFile      string // the proxy's dynamic TLS configuration, on this machine
}

// mountedCert is a certificate's files on this machine and where the proxy's
// container sees them.
type mountedCert struct {
	CertFile, KeyFile string
	Cert, Key         string
}

// mountCertificates checks the files are there before they are mounted: docker
// makes an empty directory of a bind mount's missing source, and the proxy
// then starts presenting its own certificate with nothing said.
func mountCertificates(certs []domain.Certificate) ([]mountedCert, error) {
	var out []mountedCert
	for i, c := range certs {
		for _, f := range []string{c.CertFile, c.KeyFile} {
			if info, err := os.Stat(f); err != nil {
				return nil, fmt.Errorf("proxy certificate: %w", err)
			} else if info.IsDir() {
				return nil, fmt.Errorf("proxy certificate: %s is a directory, not a PEM file", f)
			}
		}
		out = append(out, mountedCert{CertFile: c.CertFile, KeyFile: c.KeyFile,
			Cert: fmt.Sprintf("/certs/%d/cert.pem", i), Key: fmt.Sprintf("/certs/%d/key.pem", i)})
	}
	return out, nil
}

func entrypointsOf(listeners []domain.Listener) []entrypoint {
	out := make([]entrypoint, 0, len(listeners))
	for _, l := range listeners {
		bind := l.Bind
		if bind == "" {
			bind = "0.0.0.0"
		}
		out = append(out, entrypoint{Name: l.Name, Port: l.Port, Bind: bind, TLS: l.TLS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// bindRe reads the address each port is published on. A proxy written before the
// ports had addresses of their own has one for all of them, and one written before
// exposure was a choice has none: a port with nothing in front of it is every
// interface.
var bindRe = regexp.MustCompile(`- "(?:([0-9.]+):)?([0-9]+):[0-9]+"`)

var entrypointRe = regexp.MustCompile(`--entrypoints\.([^.\s]+)\.address=:(\d+)`)

var certRe = regexp.MustCompile(`- "(.+):/certs/\d+/cert\.pem:ro"`)

var keyRe = regexp.MustCompile(`- "(.+):/certs/\d+/key\.pem:ro"`)

// parseCertificatePairs reads the certificates the proxy presents back out of
// its own file, so adding one multiverse's does not drop another's.
func parseCertificatePairs(content string) []domain.Certificate {
	certs, keys := certRe.FindAllStringSubmatch(content, -1), keyRe.FindAllStringSubmatch(content, -1)
	var out []domain.Certificate
	for i, c := range certs {
		if i < len(keys) {
			out = append(out, domain.Certificate{CertFile: c[1], KeyFile: keys[i][1]})
		}
	}
	return out
}

var tlsRe = regexp.MustCompile(`--entrypoints\.([^.\s]+)\.http\.tls=true`)

func parseTLS(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range tlsRe.FindAllStringSubmatch(content, -1) {
		out[m[1]] = true
	}
	return out
}

// parseListeners reads what the proxy listens on out of its own compose file,
// so a caller can tell what starting would add to it or move.
func parseListeners(content string) []domain.Listener {
	binds := map[int]string{}
	for _, m := range bindRe.FindAllStringSubmatch(content, -1) {
		port, _ := strconv.Atoi(m[2])
		binds[port] = m[1]
	}
	tls := parseTLS(content)
	var out []domain.Listener
	for _, m := range entrypointRe.FindAllStringSubmatch(content, -1) {
		port, _ := strconv.Atoi(m[2])
		bind := binds[port]
		if bind == "" {
			bind = "0.0.0.0"
		}
		out = append(out, domain.Listener{Name: m[1], Port: port, Bind: bind, TLS: tls[m[1]]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// anyRunning accepts both compose ps shapes: one JSON object per line, or a
// single JSON array (older releases).
func anyRunning(out []byte) bool {
	out = bytes.TrimSpace(out)
	var rows []struct{ State string }
	if bytes.HasPrefix(out, []byte("[")) {
		_ = json.Unmarshal(out, &rows)
	} else {
		for _, line := range bytes.Split(out, []byte("\n")) {
			var row struct{ State string }
			if json.Unmarshal(line, &row) == nil {
				rows = append(rows, row)
			}
		}
	}
	for _, row := range rows {
		if row.State == "running" {
			return true
		}
	}
	return false
}

type label struct{ Key, Value string }

type service struct {
	Name      string
	Unpublish bool
	Ports     []string // "bind:host:container", replacing every published port
	Labels    []label
	Env       []label
	seen      map[string]bool
}

type override struct {
	Services []*service
}

// buildOverride groups routes by service so two routes on one service share a
// single entry; everything is sorted so the file is stable across runs.
func buildOverride(r app.OverrideRequest) override {
	routes, unpublish := r.Routes, r.Unpublish
	bind := r.Bind
	if bind == "" {
		bind = "0.0.0.0"
	}
	byName := map[string]*service{}
	get := func(name string) *service {
		if s, ok := byName[name]; ok {
			return s
		}
		s := &service{Name: name, seen: map[string]bool{}}
		byName[name] = s
		return s
	}
	for _, name := range unpublish {
		get(name).Unpublish = true
	}
	for _, r := range routes {
		s := get(r.Service)
		if r.HostPort > 0 {
			s.Ports = append(s.Ports, bind+":"+strconv.Itoa(r.HostPort)+":"+strconv.Itoa(r.Port))
		}
		// Reached by IP alone: no route in the proxy, so no labels and no network
		// shared with it (a shared network is where service names stop being private).
		if r.ByIP {
			continue
		}
		// The service stays on its app's own network, which is the one named in the
		// label below and the one the proxy joins. Attaching it to a network shared
		// with other universes would make its name an alias of theirs too.
		if !s.seen["enable"] {
			s.seen["enable"] = true
			s.Labels = append(s.Labels, label{"traefik.enable", "true"}, label{servedByLabel, proxyProject()})
		}
		if !s.seen["docker.network"] {
			s.seen["docker.network"] = true
			s.Labels = append(s.Labels, label{"traefik.docker.network", r.Network})
		}
		hosts := append([]string{r.Hostname}, r.Aliases...)
		rules := make([]string, len(hosts))
		for i, h := range hosts {
			rules[i] = "Host(`" + h + "`)"
		}
		prefix := "traefik.http.routers." + r.Name
		if r.Scheme != "" && r.Scheme != "http" {
			s.Labels = append(s.Labels, label{"traefik.http.services." + r.Name + ".loadbalancer.server.scheme", r.Scheme})
		}
		s.Labels = append(s.Labels,
			label{"traefik.http.services." + r.Name + ".loadbalancer.server.port", strconv.Itoa(r.Port)},
			label{prefix + ".rule", strings.Join(rules, " || ")},
			label{prefix + ".entrypoints", r.Entrypoint},
			label{prefix + ".service", r.Name},
		)
	}
	for _, se := range r.Env {
		svc := get(se.Service)
		for _, v := range se.Vars {
			svc.Env = append(svc.Env, label{v.Name, v.Value})
		}
		sort.Slice(svc.Env, func(i, j int) bool { return svc.Env[i].Key < svc.Env[j].Key })
	}
	out := override{}
	for _, s := range byName {
		out.Services = append(out.Services, s)
	}
	sort.Slice(out.Services, func(i, j int) bool { return out.Services[i].Name < out.Services[j].Name })
	return out
}

func render(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func writeIfChanged(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func proxyFile() (string, error) {
	base, err := userdirs.State()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "multiversectl", "proxy", "docker-compose.yml"), nil
}

// dockerSocket finds the daemon socket to mount into the proxy: DOCKER_HOST
// if it is a unix socket, then the rootless socket, then the system one.
func dockerSocket() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		if u, err := url.Parse(host); err == nil && u.Scheme == "unix" && u.Path != "" {
			return u.Path
		}
	}
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		sock := filepath.Join(runtime, "docker.sock")
		if info, err := os.Stat(sock); err == nil && info.Mode()&os.ModeSocket != 0 {
			return sock
		}
	}
	return "/var/run/docker.sock"
}

func (t *Traefik) docker(ctx context.Context, args ...string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker CLI not found on PATH")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = t.stdout
	cmd.Stderr = t.stderr
	done := t.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		return fmt.Errorf("docker %s: %w", strings.Join(args[:min(len(args), 2)], " "), err)
	}
	return nil
}
