package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cristiansteib/multiversectl/internal/adapters/trace"
	"github.com/cristiansteib/multiversectl/internal/app"
)

const (
	projectLabel = "com.docker.compose.project"
	serviceLabel = "com.docker.compose.service"
	workdirLabel = "com.docker.compose.project.working_dir"
	configLabel  = "com.docker.compose.project.config_files"
)

// Compose runs the container runtime through the docker CLI.
type Compose struct {
	stdout, stderr io.Writer
	trace          *trace.Tracer
}

var _ app.Runtime = (*Compose)(nil)

// New streams child output to the given writers; nil means os.Stdout / os.Stderr.
// The tracer, which may be nil, says which commands it runs.
func New(stdout, stderr io.Writer, t *trace.Tracer) *Compose {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Compose{stdout: stdout, stderr: stderr, trace: t}
}

func (c *Compose) Build(ctx context.Context, r app.RuntimeRequest) error {
	return c.stream(ctx, r, nil, "build")
}

func (c *Compose) Up(ctx context.Context, r app.RuntimeRequest) error {
	return c.stream(ctx, r, nil, append([]string{"up", "-d"}, r.Services...)...)
}

func (c *Compose) Down(ctx context.Context, r app.RuntimeRequest, removeVolumes bool) error {
	verb := []string{"down"}
	if removeVolumes {
		verb = append(verb, "-v")
	}
	return c.stream(ctx, r, nil, verb...)
}

// Run and Exec are interactive by nature: their output is the reason the user
// typed the command, so they write to the real streams even when the rest of
// the runtime's chatter is suppressed.
func (c *Compose) Run(ctx context.Context, r app.RuntimeRequest, service string, args []string) error {
	verb := append([]string{"run", "--rm", service}, args...)
	return c.attached().stream(ctx, r, os.Stdin, verb...)
}

func (c *Compose) Exec(ctx context.Context, r app.RuntimeRequest, service string, args []string) error {
	verb := append([]string{"exec", service}, args...)
	return c.attached().stream(ctx, r, os.Stdin, verb...)
}

func (c *Compose) ExecQuiet(ctx context.Context, r app.RuntimeRequest, service string, args []string) (string, error) {
	var out bytes.Buffer
	quiet := &Compose{stdout: &out, stderr: &out, trace: c.trace}
	// The variables go before the service name: after it they would be the
	// command's own arguments.
	exec := []string{"exec", "-T"}
	for _, name := range slices.Sorted(maps.Keys(r.ExecEnv)) {
		exec = append(exec, "-e", name+"="+r.ExecEnv[name])
	}
	err := quiet.stream(ctx, r, nil, append(append(exec, service), args...)...)
	return out.String(), err
}

// RemoveAsRoot runs `rm -rf` in a throwaway container with the directory's parent
// mounted: the container is root, and root can delete what another root's
// container wrote. It needs the image (pulled the first time) and nothing else:
// no network, and nothing mounted but the parent.
func (c *Compose) RemoveAsRoot(ctx context.Context, dir string) error {
	dir = filepath.Clean(dir)
	parent, name := filepath.Dir(dir), filepath.Base(dir)
	// Never a root, never a top-level folder: this is root deleting recursively.
	if !filepath.IsAbs(dir) || strings.Count(dir, string(filepath.Separator)) < 3 || name == "." || name == string(filepath.Separator) {
		return fmt.Errorf("not removing %q as root: too close to the top of the disk", dir)
	}
	_, err := c.capture(ctx, "docker", "run", "--rm", "--network", "none", "-v", parent+":/w", "alpine:3", "rm", "-rf", "/w/"+name)
	return err
}

// attached is this adapter with the process's own streams.
func (c *Compose) attached() *Compose {
	return &Compose{stdout: os.Stdout, stderr: os.Stderr, trace: c.trace}
}

func (c *Compose) Restart(ctx context.Context, r app.RuntimeRequest, service string) error {
	return c.stream(ctx, r, nil, "restart", service)
}

// Logs asks docker for the container by name, so the caller must have taken the
// name from the universe's own containers: it is never a page's word.
func (c *Compose) Logs(ctx context.Context, container string, tail int, follow bool, w io.Writer) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker CLI not found on PATH")
	}
	args := []string{"logs", "--timestamps", "--tail", strconv.Itoa(tail)}
	if follow {
		args = append(args, "--follow")
	}
	cmd := exec.CommandContext(ctx, "docker", append(args, container)...)
	// docker writes a container's stderr to its own stderr: one writer keeps
	// the lines in the order they arrived.
	lw := &lockedWriter{w: w}
	cmd.Stdout, cmd.Stderr = lw, lw
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("docker logs: %w", err)
	}
	return nil
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (c *Compose) Stats(ctx context.Context, containers []string) ([]app.ContainerStats, error) {
	if len(containers) == 0 {
		return nil, nil
	}
	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, containers...)
	out, err := c.capture(ctx, "docker", args...)
	if err == nil {
		return parseStats(out)
	}
	// A container asked for by name may have stopped since it was listed, and
	// docker then reports nothing at all rather than the rest. Reading every
	// running container and keeping the ones asked for still answers.
	if out, err = c.capture(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}"); err != nil {
		return nil, err
	}
	all, err := parseStats(out)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(containers))
	for _, name := range containers {
		wanted[name] = true
	}
	kept := all[:0]
	for _, s := range all {
		if wanted[s.Name] {
			kept = append(kept, s)
		}
	}
	return kept, nil
}

func parseStats(out []byte) ([]app.ContainerStats, error) {
	var stats []app.ContainerStats
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row struct{ Name, CPUPerc, MemPerc, MemUsage, NetIO, PIDs string }
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parsing docker stats output: %w", err)
		}
		pids, _ := strconv.Atoi(row.PIDs)
		used, limit := sizePair(row.MemUsage)
		stats = append(stats, app.ContainerStats{
			Name: row.Name, CPU: percent(row.CPUPerc), Memory: percent(row.MemPerc),
			MemUsage: row.MemUsage, MemBytes: used, MemLimit: limit, NetIO: row.NetIO, PIDs: pids,
		})
	}
	return stats, scanner.Err()
}

// percent reads docker's "12.34%"; a container that is not running reports
// "--", which is zero here.
func percent(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return v
}

// sizePair reads docker's "123.4MiB / 7.654GiB" as bytes, so a page can total
// and plot memory instead of only printing it. Either side may be missing.
func sizePair(s string) (used, limit float64) {
	before, after, _ := strings.Cut(s, "/")
	return size(before), size(after)
}

var sizeUnits = []struct {
	suffix string
	mult   float64
}{
	{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
	{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1},
}

func size(s string) float64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if rest, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				return 0
			}
			return v * u.mult
		}
	}
	return 0
}

func (c *Compose) Containers(ctx context.Context, namePrefix string) ([]app.Container, error) {
	out, err := c.capture(ctx, "docker", "ps", "--all", "--filter", "name=^"+namePrefix+"-", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var containers []app.Container
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row struct{ Names, Status, Ports, Labels string }
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parsing docker ps output: %w", err)
		}
		containers = append(containers, app.Container{
			Name:      strings.SplitN(row.Names, ",", 2)[0],
			Service:   labelValue(row.Labels, serviceLabel),
			Status:    row.Status,
			Project:   labelValue(row.Labels, projectLabel),
			Ports:     row.Ports,
			Published: publishedPorts(row.Ports),
			// Several config files are joined with commas, as labels are, so
			// what comes back is the first one: enough to say whose it is.
			WorkingDir: labelValue(row.Labels, workdirLabel),
			ConfigFile: labelValue(row.Labels, configLabel),
		})
	}
	return containers, scanner.Err()
}

// Volumes lists the named volumes whose name starts with the prefix. The
// name filter is a substring match, so the prefix is checked again here:
// project "acme-u1" must not drag in "acme-u11".
func (c *Compose) Volumes(ctx context.Context, namePrefix string) ([]app.Volume, error) {
	out, err := c.capture(ctx, "docker", "volume", "ls", "--filter", "name="+namePrefix, "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var volumes []app.Volume
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row struct{ Name, Driver, Labels string }
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parsing docker volume ls output: %w", err)
		}
		if !strings.HasPrefix(row.Name, namePrefix) {
			continue
		}
		volumes = append(volumes, app.Volume{
			Name:    row.Name,
			Project: labelValue(row.Labels, projectLabel),
			Driver:  row.Driver,
		})
	}
	return volumes, scanner.Err()
}

// Inspect reads the merged configuration: which services exist for the active
// profiles, their labels, and whether they build an image.
func (c *Compose) Inspect(ctx context.Context, r app.RuntimeRequest) (app.ProjectConfig, error) {
	cmd := exec.CommandContext(ctx, "docker", argv(r, "config", "--format", "json")...)
	cmd.Dir = r.WorkDir
	cmd.Env = os.Environ()
	for k, v := range r.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := c.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		return app.ProjectConfig{}, fmt.Errorf("docker compose config: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var parsed struct {
		Services map[string]struct {
			Labels map[string]string `json:"labels"`
			Build  json.RawMessage   `json:"build"`
			Ports  []struct {
				Target int `json:"target"`
			} `json:"ports"`
			Expose []json.Number `json:"expose"`
		} `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return app.ProjectConfig{}, fmt.Errorf("docker compose config: %w", err)
	}
	out := app.ProjectConfig{Services: make(map[string]app.ServiceConfig, len(parsed.Services))}
	for name, svc := range parsed.Services {
		seen := map[int]bool{}
		var ports []int
		for _, p := range svc.Ports {
			if p.Target != 0 && !seen[p.Target] {
				seen[p.Target] = true
				ports = append(ports, p.Target)
			}
		}
		for _, e := range svc.Expose {
			if port, err := strconv.Atoi(e.String()); err == nil && !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
		}
		sort.Ints(ports)
		out.Services[name] = app.ServiceConfig{Labels: svc.Labels, Build: len(svc.Build) > 0, Ports: ports}
	}
	return out, nil
}

func (c *Compose) Version(ctx context.Context) (string, error) {
	out, err := c.capture(ctx, "docker", "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	if _, err := c.capture(ctx, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Compose) stream(ctx context.Context, r app.RuntimeRequest, stdin io.Reader, verb ...string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker CLI not found on PATH")
	}
	cmd := exec.CommandContext(ctx, "docker", argv(r, verb...)...)
	cmd.Dir = r.WorkDir
	cmd.Env = os.Environ()
	for k, v := range r.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = stdin
	cmd.Stdout = c.stdout
	cmd.Stderr = c.stderr
	done := c.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		return fmt.Errorf("docker compose %s: %w", verb[0], err)
	}
	return nil
}

func (c *Compose) capture(ctx context.Context, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, errors.New("docker CLI not found on PATH")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	done := c.trace.Begin(cmd)
	out, err := cmd.Output()
	done(err)
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "Cannot connect to the Docker daemon") || strings.Contains(msg, "docker daemon") {
			return nil, fmt.Errorf("docker daemon is unreachable: %s", msg)
		}
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}
	return out, nil
}

func argv(r app.RuntimeRequest, verb ...string) []string {
	args := []string{"compose", "--project-name", r.Project}
	for _, f := range r.Files {
		args = append(args, "-f", f)
	}
	for _, p := range r.Profiles {
		args = append(args, "--profile", p)
	}
	return append(args, verb...)
}

// publishedPorts keeps only the host port mappings out of docker's port
// column, which lists exposed ports the same way: "5432/tcp" is reachable
// only inside the project's network, while "0.0.0.0:5432->5432/tcp" is the
// kind that stops a second universe from starting. IPv4 and IPv6 mappings of
// one container port are printed separately; one of them is the answer.
func publishedPorts(ports string) string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(ports, ",") {
		part = strings.TrimSpace(part)
		_, target, ok := strings.Cut(part, "->")
		if !ok || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, part)
	}
	return strings.Join(out, ", ")
}

// labelValue reads one key from docker ps's "k=v,k=v" label string.
func labelValue(labels, key string) string {
	for _, pair := range strings.Split(labels, ",") {
		if v, ok := strings.CutPrefix(pair, key+"="); ok {
			return v
		}
	}
	return ""
}
