// Package gitlabhost works with GitLab through its CLI, glab, so credentials
// stay wherever the user already keeps them. It talks to the REST API through
// `glab api`, which returns JSON, instead of parsing what the commands print.
package gitlabhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Host struct{}

var _ app.RepoHost = Host{}

func New() Host { return Host{} }

func (Host) Name() string { return "GitLab" }

func (Host) Available(ctx context.Context) error {
	if _, err := exec.LookPath("glab"); err != nil {
		return errors.New("the GitLab CLI (glab) is not installed")
	}
	if _, err := glab(ctx, "auth", "status"); err != nil {
		return errors.New("the GitLab CLI is not logged in; run: glab auth login")
	}
	return nil
}

// Create makes a project in the namespace the name starts with, or in the
// signed-in user's own. Visibility is private unless asked otherwise.
func (Host) Create(ctx context.Context, name string, private bool) (string, error) {
	ns, repo := path.Split(name)
	ns = strings.TrimSuffix(ns, "/")
	visibility := "private"
	if !private {
		visibility = "public"
	}
	args := []string{"api", "projects", "-X", "POST", "-f", "name=" + repo, "-f", "path=" + repo, "-f", "visibility=" + visibility}
	if ns != "" {
		id, err := namespaceID(ctx, ns)
		if err != nil {
			return "", err
		}
		args = append(args, "-f", "namespace_id="+id)
	}
	out, err := glab(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("glab: create %s: %w", name, err)
	}
	var created struct {
		HTTP string `json:"http_url_to_repo"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.HTTP == "" {
		return "", fmt.Errorf("glab: expected the new project's URL, got %q", strings.TrimSpace(out))
	}
	return created.HTTP, nil
}

func namespaceID(ctx context.Context, ns string) (string, error) {
	out, err := glab(ctx, "api", "namespaces/"+url.PathEscape(ns))
	if err != nil {
		return "", fmt.Errorf("glab: find the namespace %s: %w", ns, err)
	}
	var found struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil || found.ID == 0 {
		return "", fmt.Errorf("glab: no namespace %s", ns)
	}
	return fmt.Sprint(found.ID), nil
}

// Accounts is the signed-in user, then the groups they can create projects in
// (Developer access or more).
func (Host) Accounts(ctx context.Context) ([]app.Account, error) {
	out, err := glab(ctx, "api", "user")
	if err != nil {
		return nil, err
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(out), &user); err != nil || user.Username == "" {
		return nil, fmt.Errorf("glab: cannot tell who is signed in")
	}
	accounts := []app.Account{{Name: user.Username}}
	out, err = glab(ctx, "api", "groups?min_access_level=30&per_page=100")
	if err != nil {
		return accounts, nil
	}
	var groups []struct {
		FullPath string `json:"full_path"`
	}
	if err := json.Unmarshal([]byte(out), &groups); err == nil {
		for _, g := range groups {
			accounts = append(accounts, app.Account{Name: g.FullPath, Org: true})
		}
	}
	return accounts, nil
}

// Repos lists the projects the signed-in user is a member of in the account's
// namespace; one request covers every account, so the filter is on our side.
func (Host) Repos(ctx context.Context, account string) ([]app.RemoteRepo, error) {
	out, err := glab(ctx, "api", "projects?membership=true&simple=true&per_page=100", "--paginate")
	if err != nil {
		return nil, err
	}
	return parseProjects(out, account)
}

func parseProjects(out, account string) ([]app.RemoteRepo, error) {
	// --paginate prints one JSON array per page, back to back.
	dec := json.NewDecoder(strings.NewReader(out))
	var repos []app.RemoteRepo
	for dec.More() {
		var page []struct {
			Path        string `json:"path_with_namespace"`
			HTTP        string `json:"http_url_to_repo"`
			Description string `json:"description"`
			Visibility  string `json:"visibility"`
		}
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("read the project list: %w", err)
		}
		for _, p := range page {
			if p.Path == account || strings.HasPrefix(p.Path, account+"/") {
				repos = append(repos, app.RemoteRepo{Name: p.Path, URL: p.HTTP, Description: p.Description, Private: p.Visibility != "public"})
			}
		}
	}
	return repos, nil
}

func glab(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "glab", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}
