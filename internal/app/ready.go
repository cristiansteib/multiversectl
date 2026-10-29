package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// DefaultWaitTimeout is how long `up` waits for an app to be usable. An initial
// build or a migration can take minutes, and a timeout here is only a warning.
const DefaultWaitTimeout = 5 * time.Minute

// readyEvery is how often readiness is asked again; a variable for the tests.
var readyEvery = 2 * time.Second

// WaitOptions say how `up` waits for an app after its services are started.
type WaitOptions struct {
	// Disabled starts nothing waiting: `up` returns as soon as compose does.
	Disabled bool
	// Timeout is the most it waits, for all of an app's services and hosts
	// together; zero is DefaultWaitTimeout.
	Timeout time.Duration
	// Strict makes a timeout an error instead of a warning.
	Strict bool
}

// waitReady waits until the services of an app that have a healthcheck are
// healthy and the hosts that asked for it (ready) answer. Compose says a
// service is up when its container runs, which can be a long while before
// anything answers; the hooks that follow and the person at the terminal want
// the later moment.
//
// Not being ready in time is a warning naming what was waited for and the last
// thing it said, since the app is started either way; with Strict it is an error.
func (s *Service) waitReady(ctx context.Context, st domain.UniverseState, appKey string, req RuntimeRequest, opts WaitOptions) error {
	if opts.Disabled {
		return nil
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultWaitTimeout
	}
	deadline := time.Now().Add(timeout)

	var hosts []Route
	for _, route := range s.Routes(st, appKey) {
		if route.Ready != "" && route.Ready != "false" {
			hosts = append(hosts, route)
		}
	}

	waitingHealth := map[string]string{} // service -> what it last said
	lastHost := map[string]string{}      // hostname -> what it last said
	announced := map[string]bool{}
	for {
		pending := s.pendingHealth(ctx, req, waitingHealth)
		for _, svc := range slices.Sorted(mapKeysOf(pending)) {
			if !announced["svc:"+svc] {
				announced["svc:"+svc] = true
				s.Out.Info("%s: waiting for %s to be healthy (%s)", appKey, svc, pending[svc])
			}
		}
		var notYet []Route
		for _, route := range hosts {
			ok, why := s.hostAnswers(ctx, route)
			if ok {
				if lastHost[route.Hostname] != "ready" {
					lastHost[route.Hostname] = "ready"
					s.Out.Info("%s: %s answers", appKey, s.routeURL(route))
				}
				continue
			}
			lastHost[route.Hostname] = why
			notYet = append(notYet, route)
			if !announced["host:"+route.Hostname] {
				announced["host:"+route.Hostname] = true
				s.Out.Info("%s: waiting for %s to answer (%s)", appKey, s.routeURL(route), why)
			}
		}
		if len(pending) == 0 && len(notYet) == 0 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().Add(readyEvery).After(deadline) {
			return s.notReady(appKey, timeout, pending, notYet, lastHost, opts.Strict)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyEvery):
		}
	}
}

func mapKeysOf[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// pendingHealth is the services with a healthcheck that are not healthy yet, with
// what each says. A runtime that cannot be asked has nothing pending: waiting is
// an extra, and not being able to do it must not fail a start.
func (s *Service) pendingHealth(ctx context.Context, req RuntimeRequest, last map[string]string) map[string]string {
	rows, err := s.Runtime.Health(ctx, req)
	if err != nil {
		s.say(3, "%s: health: %v", req.Project, err)
		return nil
	}
	pending := map[string]string{}
	for _, row := range rows {
		if row.Health != "" && row.Health != "healthy" {
			pending[row.Service] = row.Health
			last[row.Service] = row.Health
		}
	}
	return pending
}

// hostAnswers asks a host's address once, the way its ready setting says, and
// says what it saw when it does not answer.
func (s *Service) hostAnswers(ctx context.Context, route Route) (bool, string) {
	base := s.routeURL(route)
	how := route.Ready
	if how == domain.ReadyTrue {
		how = ""
	}
	// A host that does not speak plain HTTP is only asked whether it listens.
	if how == "" && (route.Scheme == "h2c" || route.Probe == "tcp" || route.Probe == "grpc") {
		how = domain.ReadyTCP
	}
	if how == domain.ReadyTCP {
		address, err := tcpAddress(base)
		if err != nil {
			return false, err.Error()
		}
		if err := s.System.Dial(ctx, address); err != nil {
			return false, "no connection to " + address + ": " + shortError(err)
		}
		return true, ""
	}
	target := base
	if strings.HasPrefix(how, "/") {
		target = strings.TrimRight(base, "/") + how
	}
	answer, err := s.System.Probe(ctx, target)
	switch {
	case err != nil:
		return false, shortError(err)
	case answer.Status == 404 && isProxyNotFound(answer.Body):
		return false, "the proxy has no route for it yet"
	case answer.Status >= 500:
		return false, fmt.Sprintf("status %d", answer.Status)
	}
	return true, ""
}

// tcpAddress is host:port of an address, with the scheme's port when it names none.
func tcpAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("not an address: %q", raw)
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

// shortError keeps a connection error to what a person can use: Go's wrap of
// the URL and the dial adds nothing to what the address already says.
func shortError(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.Contains(msg, "dial tcp") {
		return msg[i+2:]
	}
	return msg
}

func (s *Service) notReady(appKey string, timeout time.Duration, pending map[string]string, hosts []Route, last map[string]string, strict bool) error {
	var parts []string
	for _, svc := range slices.Sorted(mapKeysOf(pending)) {
		parts = append(parts, fmt.Sprintf("service %s is still %s", svc, pending[svc]))
	}
	for _, route := range hosts {
		parts = append(parts, fmt.Sprintf("%s did not answer (last: %s)", s.routeURL(route), last[route.Hostname]))
	}
	msg := fmt.Sprintf("%s: not ready after %s: %s", appKey, timeout.Round(time.Second), strings.Join(parts, "; "))
	if strict {
		return fmt.Errorf("%s", msg)
	}
	s.Out.Warn("%s; going on, as it may still be starting (--wait-timeout waits longer, --wait-strict makes this an error)", msg)
	return nil
}
