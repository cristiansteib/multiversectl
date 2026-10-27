// Package githost works with GitHub through the GitHub CLI, so credentials
// stay wherever the user already keeps them.
package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Host struct{}

var _ app.RepoHost = Host{}

func New() Host { return Host{} }

func (Host) Name() string { return "GitHub" }

func (Host) Available(ctx context.Context) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return &app.HostUnavailable{Reason: "the GitHub CLI (gh) is not installed: https://cli.github.com"}
	}
	if err := exec.CommandContext(ctx, "gh", "auth", "status").Run(); err != nil {
		return &app.HostUnavailable{Reason: "not signed in", CanSignIn: true}
	}
	return nil
}

// Login hands the terminal to gh, which shows a one-time code and opens the
// browser, then sets git up to push with the same credentials.
func (Host) Login(ctx context.Context) error {
	login := exec.CommandContext(ctx, "gh", "auth", "login", "--web", "--git-protocol", "https", "--hostname", "github.com")
	login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}
	setup := exec.CommandContext(ctx, "gh", "auth", "setup-git")
	setup.Stdout, setup.Stderr = os.Stderr, os.Stderr
	_ = setup.Run() // pushing may still work through another credential helper
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

// ReposWithTopic asks each account in turn, in parallel. GitHub's own search
// leaves out private repositories, which is where a root usually is.
func (h Host) ReposWithTopic(ctx context.Context, topic string) ([]app.RemoteRepo, error) {
	accounts, err := h.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	results := make([][]app.RemoteRepo, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := gh(ctx, "repo", "list", a.Name, "--topic", topic, "--limit", "50", "--json", "nameWithOwner,url,description,isPrivate")
			if err != nil {
				return // an account that cannot be listed has nothing to offer
			}
			results[i], _ = parseRepos(out)
		}()
	}
	wg.Wait()
	var all []app.RemoteRepo
	for _, r := range results {
		all = append(all, r...)
	}
	return all, nil
}

func (Host) AddTopic(ctx context.Context, repoURL, topic string) error {
	_, err := gh(ctx, "repo", "edit", repoURL, "--add-topic", topic)
	return err
}

func (Host) Owns(repoURL string) bool { return strings.Contains(repoURL, "github.com") }

// FindReview asks for the pull requests a branch has had. The open one is the
// answer when there is one; otherwise the latest that was merged or closed,
// because a branch whose request has landed is a different thing to look at
// than one nobody has proposed.
func (Host) FindReview(ctx context.Context, repoURL, branch string) (*app.Review, error) {
	if branch == "" {
		return nil, nil
	}
	out, err := gh(ctx, "pr", "list", "--repo", repoURL, "--head", branch, "--state", "all",
		"--limit", "10", "--json", "number,title,url,isDraft,state,baseRefName,author,updatedAt")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		IsDraft     bool   `json:"isDraft"`
		State       string `json:"state"`
		BaseRefName string `json:"baseRefName"`
		UpdatedAt   string `json:"updatedAt"`
		Author      struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("read the pull request list: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	// The list is newest first, so without an open one the first is the latest.
	pick := raw[0]
	for _, r := range raw {
		if r.State == "OPEN" {
			pick = r
			break
		}
	}
	updated, _ := time.Parse(time.RFC3339, pick.UpdatedAt)
	state := strings.ToLower(pick.State)
	if state == "open" && pick.IsDraft {
		state = "draft"
	}
	return &app.Review{Number: pick.Number, Title: pick.Title, URL: pick.URL, State: state, Draft: state == "draft",
		Base: pick.BaseRefName, Author: pick.Author.Login, Updated: updated}, nil
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
		// gh usually explains itself on stderr. When it says nothing at all,
		// the exit status is all there is, and an error whose message is empty
		// reaches the user as a bare "error:".
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return "", fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}
