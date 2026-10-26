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
	// Follows is the branch of origin it is compared with, and Behind and Ahead
	// the commits it lacks and has, as of the last fetch.
	Follows string `json:"follows,omitempty"`
	Behind  int    `json:"behind,omitempty"`
	Ahead   int    `json:"ahead,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
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
		cv := CheckoutView{Repo: c.Repo, Branch: c.Branch, Path: c.Path}
		if f := c.Freshness; f != nil {
			cv.Follows, cv.Behind, cv.Ahead, cv.Dirty = f.Follows, f.Behind, f.Ahead, f.Dirty
		}
		v.Checkouts = append(v.Checkouts, cv)
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

// BriefView is what an agent reads to find its way, and nothing more: it is the
// default `-o json`, because every token a listing costs is one the agent does
// not have for the work. The detail (services, volumes, compose names, times)
// is `-o json,wide`.
type BriefView struct {
	Project   BriefProject    `json:"project"`
	Universes []BriefUniverse `json:"universes"`
}

// BriefProject is where the project lives.
type BriefProject struct {
	Name         string            `json:"name"`
	Root         string            `json:"root"`
	Manifest     string            `json:"manifest,omitempty"`
	UniversesDir string            `json:"universesDir"`
	Clones       map[string]string `json:"clones,omitempty"`
}

// BriefUniverse is one universe: whether it runs, where to edit, where to browse.
type BriefUniverse struct {
	Name  string `json:"name"`
	State string `json:"state"`
	// Up is "running/expected" services.
	Up       string            `json:"up"`
	Dir      string            `json:"dir"`
	Checkout map[string]string `json:"checkout"`
	Branch   map[string]string `json:"branch"`
	URLs     []string          `json:"urls"`
	// Down names the services that should be up and are not, so a healthy
	// universe costs nothing and a broken one says where to look.
	Down []string `json:"down,omitempty"`
	// Behind is, per repository, how many commits of the branch it follows the
	// checkout lacks. Absent when it is current: `universe sync` fixes it.
	Behind map[string]int `json:"behind,omitempty"`
	Note   string         `json:"note,omitempty"`
}

func briefView(c app.ProjectContext, list []app.Summary) BriefView {
	v := BriefView{
		Project:   BriefProject{Name: c.Name, Root: c.Root, Manifest: c.Manifest, UniversesDir: c.UniversesDir},
		Universes: make([]BriefUniverse, 0, len(list)),
	}
	if len(c.Repos) > 0 {
		v.Project.Clones = map[string]string{}
		for _, r := range c.Repos {
			v.Project.Clones[r.Key] = r.Path
		}
	}
	for _, u := range list {
		state, _, up := standing(u)
		b := BriefUniverse{
			Name: u.Name, State: state, Up: up, Dir: u.Dir, Note: u.Note,
			Checkout: map[string]string{}, Branch: u.Branches, URLs: []string{},
		}
		if b.Branch == nil {
			b.Branch = map[string]string{}
		}
		for _, c := range u.Checkouts {
			b.Checkout[c.Repo] = c.Path
			if c.Freshness != nil && c.Freshness.Behind > 0 {
				if b.Behind == nil {
					b.Behind = map[string]int{}
				}
				b.Behind[c.Repo] = c.Freshness.Behind
			}
		}
		for _, a := range u.Apps {
			for _, h := range a.URLs {
				b.URLs = append(b.URLs, h.URL)
			}
			for _, sv := range a.Services {
				if sv.Counted() && !sv.Up() && u.Running > 0 {
					b.Down = append(b.Down, a.Key+"/"+sv.Name)
				}
			}
		}
		v.Universes = append(v.Universes, b)
	}
	return v
}

// writeCompactJSON is for the agent's context: no indentation.
func writeCompactJSON(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
