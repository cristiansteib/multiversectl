package domain

import "time"

// RouteRule is one resolved address of a universe: which app and service
// answer it, on which proxy entrypoint. It is stored so reading commands need
// no container runtime.
type RouteRule struct {
	App        string   `yaml:"app"`
	Service    string   `yaml:"service"`
	Port       int      `yaml:"port"`
	Entrypoint string   `yaml:"entrypoint"`
	Aliases    []string `yaml:"aliases,omitempty"`
	Hostname   string   `yaml:"hostname"`
}

// UniverseState is what survives between commands: enough to act on a
// universe without re-deriving anything that could have changed meanwhile,
// plus the few facts about it that nothing else records.
type UniverseState struct {
	// Name is as the caller typed it; ID and Slug are written out although
	// they are derived from it, because this file is read by people and by
	// other tools, and a grep for a hostname should find the universe.
	Name string `yaml:"name"`
	ID   string `yaml:"id"`
	Slug string `yaml:"slug"`
	// Number is set for a numbered universe only. It is also how state
	// written before universes could be named still loads.
	Number int `yaml:"number,omitempty"`

	Domain   string               `yaml:"domain"`
	Project  string               `yaml:"project"`
	Branches map[string]string    `yaml:"branches"`
	Hosts    map[string]string    `yaml:"hosts"`
	Rules    map[string]RouteRule `yaml:"rules"`

	// What a universe sitting there for a week needs to be identifiable
	// again: when it appeared, when it last ran, why it exists, and what
	// made it.
	Created     time.Time `yaml:"created"`
	LastStarted time.Time `yaml:"lastStarted,omitempty"`
	Note        string    `yaml:"note,omitempty"`
	Manifest    string    `yaml:"manifest,omitempty"`
	Version     string    `yaml:"version,omitempty"`
}

// Universe rebuilds the identity this state belongs to.
func (st UniverseState) Universe(workspace string, m *Manifest) Universe {
	return Universe{Name: st.Name, Workspace: workspace, Manifest: m}
}
