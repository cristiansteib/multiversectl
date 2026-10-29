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
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

const proxyProject = "multiverse-proxy"

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

func (t *Traefik) EnsureUp(ctx context.Context, r app.ProxyRequest) error {
	file, err := proxyFile()
	if err != nil {
		return err
	}
	bind := r.Bind
	if bind == "" {
		bind = "0.0.0.0"
	}
	eps := sortedEntrypoints(r.Entrypoints)
	for i := range eps {
		eps[i].TLS = r.TLS[eps[i].Name]
	}
	certs, err := mountCertificates(r.Certificates)
	if err != nil {
		return err
	}
	data := proxyData{Network: r.Network, Socket: dockerSocket(), Bind: bind, Entrypoints: eps,
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
	return t.docker(ctx, "compose", "--project-name", proxyProject, "-f", file, "up", "-d")
}

func (t *Traefik) Down(ctx context.Context) error {
	file, err := proxyFile()
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	return t.docker(ctx, "compose", "--project-name", proxyProject, "-f", file, "down")
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
	st.Entrypoints = parseEntrypoints(string(content))
	st.Bind = parseBind(string(content))
	st.TLS = parseTLS(string(content))
	st.Certificates = parseCertificates(string(content))

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "compose", "--project-name", proxyProject, "-f", file, "ps", "--format", "{{json .}}")
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
	TLS  bool
}

type proxyData struct {
	Network      string
	Socket       string
	Bind         string
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

func sortedEntrypoints(m map[string]int) []entrypoint {
	out := make([]entrypoint, 0, len(m))
	for name, port := range m {
		out = append(out, entrypoint{Name: name, Port: port})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var bindRe = regexp.MustCompile(`- "([0-9.]+):[0-9]+:[0-9]+"`)

var entrypointRe = regexp.MustCompile(`--entrypoints\.([^.\s]+)\.address=:(\d+)`)

var certRe = regexp.MustCompile(`- "(.+):/certs/\d+/cert\.pem:ro"`)

func parseCertificates(content string) []string {
	var out []string
	for _, m := range certRe.FindAllStringSubmatch(content, -1) {
		out = append(out, m[1])
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

// parseBind reads the address the proxy publishes on out of its own compose
// file, so a caller can tell that starting would move it.
func parseBind(content string) string {
	if m := bindRe.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}

func parseEntrypoints(content string) map[string]int {
	out := map[string]int{}
	for _, m := range entrypointRe.FindAllStringSubmatch(content, -1) {
		port, _ := strconv.Atoi(m[2])
		out[m[1]] = port
	}
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
	Networks  []string
	seen      map[string]bool
}

type override struct {
	Services []*service
	Networks []string
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
	networks := map[string]bool{}
	for _, name := range unpublish {
		get(name).Unpublish = true
	}
	for _, r := range routes {
		s := get(r.Service)
		if r.HostPort > 0 {
			s.Ports = append(s.Ports, bind+":"+strconv.Itoa(r.HostPort)+":"+strconv.Itoa(r.Port))
		}
		networks[r.Network] = true
		if !s.seen[r.Network] {
			s.seen[r.Network] = true
			s.Networks = append(s.Networks, r.Network)
		}
		if !s.seen["enable"] {
			s.seen["enable"] = true
			s.Labels = append(s.Labels, label{"traefik.enable", "true"})
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
		sort.Strings(s.Networks)
		out.Services = append(out.Services, s)
	}
	sort.Slice(out.Services, func(i, j int) bool { return out.Services[i].Name < out.Services[j].Name })
	for n := range networks {
		out.Networks = append(out.Networks, n)
	}
	sort.Strings(out.Networks)
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
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
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
