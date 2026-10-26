package server

import (
	"os"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// The JSON the UI reads. These are written for the page, not borrowed from the
// app types: the page is a client with its own lifetime, and what it is shown
// should not change shape because a use case's struct did.

type catalogView struct {
	Root     nodeView `json:"root"`
	Installs int      `json:"installed"`
}

// locView is where something lives, for the page to draw with the platform's own
// icon: the notation is computed here, once, so the page does not parse URLs.
type locView struct {
	Platform string `json:"platform"`
	Short    string `json:"short"`
	Path     string `json:"path"`
	Host     string `json:"host,omitempty"`
	Ref      string `json:"ref,omitempty"`
	URL      string `json:"url,omitempty"`
}

func locOf(l domain.Location) *locView {
	if l.Platform == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	return &locView{Platform: string(l.Platform), Short: l.Short(home), Path: l.Path, Host: l.Host, Ref: l.Ref, URL: l.URL}
}

type nodeView struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"` // catalog or project
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	File        string `json:"file,omitempty"`
	InstalledAs string `json:"installedAs,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
	// Loc is where the node lives and Within the file inside it, when that is
	// not the default: two catalogs in one repository differ only by it.
	Loc      *locView   `json:"loc,omitempty"`
	Within   string     `json:"within,omitempty"`
	Children []nodeView `json:"children,omitempty"`
}

type fleetView struct {
	Workspaces []fleetWorkspace `json:"projects"`
	Error      string           `json:"error,omitempty"`
}

type fleetWorkspace struct {
	Name      string         `json:"name"`
	Current   bool           `json:"current,omitempty"`
	Root      string         `json:"root"`
	Universes []universeView `json:"universes"`
	Error     string         `json:"error,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
}

type universeView struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Project     string            `json:"project"`
	Domain      string            `json:"domain"`
	Dir         string            `json:"dir"`
	Running     int               `json:"running"`
	Created     string            `json:"created,omitempty"`
	LastStarted string            `json:"lastStarted,omitempty"`
	Note        string            `json:"note,omitempty"`
	PR          string            `json:"pr,omitempty"`
	Branches    map[string]string `json:"branches,omitempty"`
	// Behind is the commits its checkouts lack of the branches they follow,
	// summed; `sync` brings them forward.
	Behind    int            `json:"behind,omitempty"`
	Apps      []appView      `json:"apps"`
	Checkouts []checkoutView `json:"checkouts"`
}

type appView struct {
	Key      string        `json:"key"`
	Repo     string        `json:"repo"`
	Branch   string        `json:"branch,omitempty"`
	Path     string        `json:"path"`
	Running  int           `json:"running"`
	Expected int           `json:"expected"`
	URLs     []urlView     `json:"urls,omitempty"`
	Services []serviceView `json:"services,omitempty"`
}

type urlView struct {
	Host    string `json:"host"`
	Service string `json:"service"`
	URL     string `json:"url"`
}

type serviceView struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	Published string `json:"published,omitempty"`
}

type checkoutView struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path"`
	App    string `json:"app,omitempty"`
	// Against the branch it follows, as last fetched.
	Follows string `json:"follows,omitempty"`
	Behind  int    `json:"behind,omitempty"`
	Ahead   int    `json:"ahead,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
}

type workspaceView struct {
	Name    string `json:"name"`
	Current bool   `json:"current,omitempty"`
	Root    string `json:"root"`
	// Where its pieces are, so nobody has to go looking: the definition file, where
	// it was installed from and the folder of the universes.
	Manifest     string            `json:"manifest,omitempty"`
	From         string            `json:"from,omitempty"`
	Source       string            `json:"source,omitempty"`
	UniversesDir string            `json:"universesDir,omitempty"`
	Domain       string            `json:"domain"`
	Expose       string            `json:"expose"`
	Network      string            `json:"network"`
	Proxy        map[string]int    `json:"entrypoints"`
	Repos        []repoView        `json:"repos"`
	Apps         []appDefView      `json:"apps"`
	Env          map[string]string `json:"env,omitempty"`
}

type repoView struct {
	Key string `json:"key"`
	// Clone is the shared clone's path: where secrets go and every universe branches from.
	Clone   string   `json:"clone,omitempty"`
	Loc     *locView `json:"loc,omitempty"`
	URL     string   `json:"url"`
	Dir     string   `json:"dir"`
	Runtime string   `json:"runtime"`
	Cloned  bool     `json:"cloned"`
	// How the clone stands against origin's default branch, as last fetched.
	Follows string `json:"follows,omitempty"`
	Behind  int    `json:"behind,omitempty"`
	Ahead   int    `json:"ahead,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
}

type appDefView struct {
	Key      string        `json:"key"`
	Repo     string        `json:"repo"`
	Services []string      `json:"services,omitempty"`
	Optional []string      `json:"optional,omitempty"`
	Hosts    []hostDefView `json:"hosts,omitempty"`
}

type hostDefView struct {
	Name       string `json:"name"`
	Service    string `json:"service"`
	Port       int    `json:"port"`
	Entrypoint string `json:"entrypoint"`
}

type doctorView struct {
	Checks []checkView `json:"checks"`
	OK     bool        `json:"ok"`
}

type checkView struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type overviewView struct {
	Version  string           `json:"version"`
	ReadOnly bool             `json:"readOnly,omitempty"`
	Status   app.StatusReport `json:"status"`
	// RootLoc is where the root catalog lives and Sources where each installed
	// project's manifest came from, by project name.
	RootLoc *locView           `json:"rootLoc,omitempty"`
	Sources map[string]locView `json:"sources,omitempty"`
}

func overviewOf(version string, readOnly bool, rep app.StatusReport) overviewView {
	v := overviewView{Version: version, ReadOnly: readOnly, Status: rep, RootLoc: locOf(domain.Locate(rep.Root.Source, "")), Sources: map[string]locView{}}
	for _, w := range rep.Workspaces {
		if l := locOf(domain.Locate(w.RepoURL, w.Ref)); l != nil {
			v.Sources[w.Name] = *l
		}
	}
	return v
}

// helloView is the first thing a page is told on the websocket.
type helloView struct {
	Version  string   `json:"version"`
	Commit   string   `json:"commit,omitempty"`
	Date     string   `json:"date,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
	Ops      []opView `json:"ops"`
}

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// systemView is what this machine runs on: this server, the command line beside
// it, the tools they drive, and whether a newer release exists.
type systemView struct {
	Server   versionInfo   `json:"server"`
	Tools    []toolVersion `json:"tools"`
	Latest   latestView    `json:"latest"`
	Format   string        `json:"format"`
	Listen   string        `json:"listen,omitempty"`
	Started  string        `json:"started,omitempty"`
	ReadOnly bool          `json:"readOnly,omitempty"`
}

type toolVersion struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Note    string `json:"note,omitempty"` // why it is missing, or what it is for
	OK      bool   `json:"ok"`
}

type latestView struct {
	Version string `json:"version,omitempty"`
	Newer   bool   `json:"newer,omitempty"`
	Error   string `json:"error,omitempty"`
}

// nextView is the number a new universe would take, and why it passed others.
type nextView struct {
	Number  int      `json:"number"`
	Skipped []string `json:"skipped,omitempty"`
}

type branchView struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}
