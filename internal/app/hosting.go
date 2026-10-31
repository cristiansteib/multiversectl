package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// hostedRepos groups the manifest's repositories by the hosting service
// instance they are on, each service pointed at its instance. A repository on
// no known service (a plain git host, a local path) is on none.
type hostedRepos struct {
	host     RepoHost
	instance string // "" is the service's public one
	repos    []string
}

func (s *Service) hostedRepos() []hostedRepos {
	var out []hostedRepos
	index := map[string]int{}
	for _, key := range sortedRepoKeys(s.Manifest.Repos) {
		url := s.Manifest.Repos[key].URL
		for _, h := range s.Hosts {
			if !h.Owns(url) {
				continue
			}
			instance := domain.Locate(url, "").Host
			if at, ok := h.(HostAt); ok && instance != "" {
				h = at.At(instance)
			}
			id := h.Name() + "@" + instance
			i, seen := index[id]
			if !seen {
				i = len(out)
				index[id] = i
				out = append(out, hostedRepos{host: h, instance: instance})
			}
			out[i].repos = append(out[i].repos, key)
			break
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].host.Name()+out[a].instance < out[b].host.Name()+out[b].instance })
	return out
}

// where names an instance for a message: its host, or the public service.
func where(h RepoHost, instance string) string {
	if instance == "" {
		return h.Name()
	}
	return h.Name() + " on " + instance
}

// unavailableText says why a service cannot be asked and what to do about it.
func unavailableText(h RepoHost, instance string, err error) string {
	text := where(h, instance) + ": " + err.Error()
	var unavailable *HostUnavailable
	if errors.As(err, &unavailable) && unavailable.Fix != "" {
		text += "; run `" + unavailable.Fix + "`"
	}
	return text
}

// checkHosting reports whether each service the repositories are on can be
// asked about their pull and merge requests. Without it a branch simply shows
// none, which looks the same as a branch that never had one.
func (s *Service) checkHosting(ctx context.Context, add addCheck) {
	ready := &readiness{}
	for _, g := range s.hostedRepos() {
		name := strings.ToLower(g.host.Name())
		if g.instance != "" {
			name += " " + g.instance
		}
		if err := ready.check(ctx, g.host, g.instance); err != nil {
			detail := fmt.Sprintf("%s; the pull and merge requests of %s cannot be shown until it is", unavailableText(g.host, g.instance, err), strings.Join(g.repos, ", "))
			// A tool that is not installed is said, but is not something to be
			// told off for: nobody has to look at reviews from here. A tool that is
			// there and not signed in is one step from working.
			var unavailable *HostUnavailable
			if errors.As(err, &unavailable) && unavailable.CanSignIn {
				add(name, StatusWarn, "%s", detail)
			} else {
				add(name, StatusOK, "%s", detail)
			}
			continue
		}
		add(name, StatusOK, "signed in, for %s", strings.Join(g.repos, ", "))
	}
}
