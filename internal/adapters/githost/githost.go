// Package githost works with GitHub through the GitHub CLI, so credentials
// stay wherever the user already keeps them.
package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Host struct{}

var _ app.RepoHost = Host{}

func New() Host { return Host{} }

func (Host) Name() string { return "GitHub" }

func (Host) Available(ctx context.Context) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI (gh) is not installed")
	}
	if err := exec.CommandContext(ctx, "gh", "auth", "status").Run(); err != nil {
		return errors.New("the GitHub CLI is not logged in; run: gh auth login")
	}
	return nil
}

// Create is private unless asked otherwise: a catalog lists where a person's
// projects live, which is not for everyone.
func (Host) Create(ctx context.Context, name string, private bool) (string, error) {
	visibility := "--private"
	if !private {
		visibility = "--public"
	}
	out, err := gh(ctx, "repo", "create", name, visibility)
	if err != nil {
		return "", fmt.Errorf("gh repo create: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(url, "http") {
		return "", fmt.Errorf("gh repo create: expected the new repository's URL, got %q", url)
	}
	return url, nil
}

// Accounts is the signed-in user, then the organizations they belong to.
func (Host) Accounts(ctx context.Context) ([]app.Account, error) {
	login, err := gh(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return nil, err
	}
	accounts := []app.Account{{Name: strings.TrimSpace(login)}}
	orgs, err := gh(ctx, "api", "user/orgs", "--paginate", "--jq", ".[].login")
	if err != nil {
		return accounts, nil // the user's own account is still a place to create
	}
	for _, o := range strings.Fields(orgs) {
		accounts = append(accounts, app.Account{Name: o, Org: true})
	}
	return accounts, nil
}

func (Host) Repos(ctx context.Context, account string) ([]app.RemoteRepo, error) {
	out, err := gh(ctx, "repo", "list", account, "--limit", "300", "--json", "nameWithOwner,url,description,isPrivate")
	if err != nil {
		return nil, err
	}
	return parseRepos(out)
}

func parseRepos(out string) ([]app.RemoteRepo, error) {
	var raw []struct {
		NameWithOwner string `json:"nameWithOwner"`
		URL           string `json:"url"`
		Description   string `json:"description"`
		IsPrivate     bool   `json:"isPrivate"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("read the repository list: %w", err)
	}
	repos := make([]app.RemoteRepo, len(raw))
	for i, r := range raw {
		repos[i] = app.RemoteRepo{Name: r.NameWithOwner, URL: r.URL, Description: r.Description, Private: r.IsPrivate}
	}
	return repos, nil
}

// gh runs the CLI and returns its words, not a bare exit status, when it fails.
func gh(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", errors.New(strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
