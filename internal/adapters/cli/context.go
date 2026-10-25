package cli

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// projectHeader says which project a listing is about and where its pieces are,
// so the next command, or an agent that has only this output, does not have to
// go looking: the root, the definition, where it came from, the clones.
func projectHeader(o *Output, c app.ProjectContext, wide bool) {
	name := o.paint(bold+cyan, "project "+c.Name)
	if c.Current {
		name += o.Dim(" (current)")
	}
	o.Plain("%s", name+o.Dim("  ·  root "+shortPath(c.Root)))
	if c.Manifest != "" {
		line := "definition " + shortPath(c.Manifest)
		if from := originLine(c); from != "" {
			line += "  ·  " + from
		}
		o.Plain("%s", o.Dim(line))
	}
	o.Plain("%s", o.Dim("universes "+shortPath(c.UniversesDir)))
	if wide && len(c.Repos) > 0 {
		o.Plain("")
		rows := make([][]Cell, 0, len(c.Repos))
		for _, r := range c.Repos {
			rows = append(rows, []Cell{{Text: r.Key}, {Text: r.Runtime}, {Text: shortPath(r.Path)}, {Text: r.URL, Style: dim}})
		}
		o.Block(o.Dim("clones (shared; secrets go here, every universe branches from them)"), []string{"REPO", "RUNTIME", "CLONE", "URL"}, rows)
	}
	o.Plain("")
}

// originLine is where the project came from, in one phrase.
func originLine(c app.ProjectContext) string {
	var parts []string
	if c.From != "" {
		parts = append(parts, "from "+c.From)
	}
	if c.Source != "" {
		parts = append(parts, c.Source)
	}
	return strings.Join(parts, " · ")
}

// UniverseView is a universe as a script or an agent reads it: every path it
// would otherwise have to work out, and every address.
type UniverseView struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Compose     string            `json:"compose"`
	Domain      string            `json:"domain"`
	Dir         string            `json:"dir"`
	Running     int               `json:"running"`
	Branches    map[string]string `json:"branches"`
	Hosts       map[string]string `json:"hosts"`
	Apps        []AppView         `json:"apps"`
	Checkouts   []CheckoutView    `json:"checkouts"`
	Created     *time.Time        `json:"created,omitempty"`
	LastStarted *time.Time        `json:"lastStarted,omitempty"`
	Note        string            `json:"note,omitempty"`
}

// AppView is one app of a universe.
type AppView struct {
	Key      string        `json:"key"`
	Repo     string        `json:"repo"`
	Branch   string        `json:"branch"`
	Path     string        `json:"path"`
	Compose  string        `json:"compose"`
	Running  int           `json:"running"`
	Expected int           `json:"expected,omitempty"`
	URLs     []URLView     `json:"urls"`
	Services []ServiceView `json:"services,omitempty"`
	Volumes  []string      `json:"volumes,omitempty"`
}

// URLView is one address.
type URLView struct {
	Host    string `json:"host"`
	Service string `json:"service"`
	URL     string `json:"url"`
}

// ServiceView is one service and where it stands.
type ServiceView struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	Published string `json:"published,omitempty"`
}

// CheckoutView is one repository's worktree in a universe: the directory to edit in.
type CheckoutView struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// ListView is what `universe ls -o json` prints: the project and its universes.
type ListView struct {
	Project   app.ProjectContext `json:"project"`
	Universes []UniverseView     `json:"universes"`
}

func universeView(u app.Summary) UniverseView {
	v := UniverseView{
		Name: u.Name, Slug: u.Slug, Compose: u.Project, Domain: u.Domain, Dir: u.Dir, Running: u.Running,
		Branches: u.Branches, Hosts: u.Hosts, Note: u.Note,
		Apps: []AppView{}, Checkouts: []CheckoutView{},
	}
	if !u.Created.IsZero() {
		v.Created = &u.Created
	}
	if !u.LastStarted.IsZero() {
		v.LastStarted = &u.LastStarted
	}
	for _, a := range u.Apps {
		av := AppView{Key: a.Key, Repo: a.Repo, Branch: a.Branch, Path: a.Path, Compose: a.Project, Running: a.Running, Expected: a.Expected, Volumes: a.Volumes, URLs: []URLView{}}
		for _, h := range a.URLs {
			av.URLs = append(av.URLs, URLView{Host: h.Host, Service: h.Service, URL: h.URL})
		}
		for _, sv := range a.Services {
			av.Services = append(av.Services, ServiceView{Name: sv.Name, State: string(sv.State), Detail: sv.Detail, Published: sv.Published})
		}
		v.Apps = append(v.Apps, av)
	}
	for _, c := range u.Checkouts {
		v.Checkouts = append(v.Checkouts, CheckoutView{Repo: c.Repo, Branch: c.Branch, Path: c.Path})
	}
	return v
}

func listView(c app.ProjectContext, list []app.Summary) ListView {
	v := ListView{Project: c, Universes: make([]UniverseView, 0, len(list))}
	for _, u := range list {
		v.Universes = append(v.Universes, universeView(u))
	}
	return v
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
