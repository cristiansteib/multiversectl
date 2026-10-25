package server

import "github.com/cristiansteib/multiversectl/internal/app"

// The JSON the UI reads. These are written for the page, not borrowed from the
// app types: the page is a client with its own lifetime, and what it is shown
// should not change shape because a use case's struct did.

type catalogView struct {
	Root     nodeView `json:"root"`
	Installs int      `json:"installed"`
}

type nodeView struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Kind        string     `json:"kind"` // catalog or workspace
	Repo        string     `json:"repo,omitempty"`
	Ref         string     `json:"ref,omitempty"`
	File        string     `json:"file,omitempty"`
	InstalledAs string     `json:"installedAs,omitempty"`
	Unavailable string     `json:"unavailable,omitempty"`
	Children    []nodeView `json:"children,omitempty"`
}

type fleetView struct {
	Workspaces []fleetWorkspace `json:"workspaces"`
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
	Branches    map[string]string `json:"branches,omitempty"`
	Apps        []appView         `json:"apps"`
	Checkouts   []checkoutView    `json:"checkouts"`
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
}

type workspaceView struct {
	Name    string            `json:"name"`
	Current bool              `json:"current,omitempty"`
	Root    string            `json:"root"`
	Domain  string            `json:"domain"`
	Expose  string            `json:"expose"`
	Network string            `json:"network"`
	Proxy   map[string]int    `json:"entrypoints"`
	Repos   []repoView        `json:"repos"`
	Apps    []appDefView      `json:"apps"`
	Env     map[string]string `json:"env,omitempty"`
}

type repoView struct {
	Key     string `json:"key"`
	URL     string `json:"url"`
	Dir     string `json:"dir"`
	Runtime string `json:"runtime"`
	Cloned  bool   `json:"cloned"`
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
