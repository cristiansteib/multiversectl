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
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

type Host struct{}

var _ app.RepoHost = Host{}

func New() Host { return Host{} }

func (Host) Name() string { return "GitLab" }

func (Host) Available(ctx context.Context) error {
	if _, err := exec.LookPath("glab"); err != nil {
		return &app.HostUnavailable{Reason: "the GitLab CLI (glab) is not installed: https://gitlab.com/gitlab-org/cli"}
	}
	if _, err := glab(ctx, "auth", "status"); err != nil {
		return &app.HostUnavailable{Reason: "not signed in", CanSignIn: true}
	}
	return nil
}

// Login hands the terminal to glab, which offers a browser sign-in.
func (Host) Login(ctx context.Context) error {
	login := exec.CommandContext(ctx, "glab", "auth", "login")
	login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("glab auth login: %w", err)
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
			if account == "" || p.Path == account || strings.HasPrefix(p.Path, account+"/") {
				repos = append(repos, app.RemoteRepo{Name: p.Path, URL: p.HTTP, Description: p.Description, Private: p.Visibility != "public"})
			}
		}
	}
	return repos, nil
}

// ReposWithTopic is one request: the projects the user is a member of that
// carry the topic.
func (Host) ReposWithTopic(ctx context.Context, topic string) ([]app.RemoteRepo, error) {
	out, err := glab(ctx, "api", "projects?membership=true&simple=true&per_page=100&topic="+url.QueryEscape(topic), "--paginate")
	if err != nil {
		return nil, err
	}
	return parseProjects(out, "")
}

// projectID is how the API addresses a project: its group-and-name path, with
// the slashes escaped. Reading it off the URL by hand only worked for an
// https one; an scp-style `git@gitlab.com:me/index.git`, the form GitLab
// offers for cloning, came out as the bare `index`, which addresses either
// nothing or somebody else's project.
func projectID(repoURL string) (string, error) {
	l := domain.Locate(repoURL, "")
	if l.Platform != domain.PlatformGitLab || l.Path == "" {
		return "", fmt.Errorf("%s is not a GitLab project", repoURL)
	}
	return url.PathEscape(l.Path), nil
}

// AddTopic keeps the topics the project has: setting them replaces the list.
func (Host) AddTopic(ctx context.Context, repoURL, topic string) error {
	id, err := projectID(repoURL)
	if err != nil {
		return err
	}
	out, err := glab(ctx, "api", "projects/"+id)
	if err != nil {
		return err
	}
	var project struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal([]byte(out), &project); err != nil {
		return fmt.Errorf("glab: read the project: %w", err)
	}
	for _, t := range project.Topics {
		if t == topic {
			return nil
		}
	}
	args := []string{"api", "projects/" + id, "-X", "PUT"}
	for _, t := range append(project.Topics, topic) {
		args = append(args, "-f", "topics[]="+t)
	}
	_, err = glab(ctx, args...)
	return err
}

// Owns asks which platform the URL names, rather than looking for "gitlab"
// anywhere in it: a repository called gitlab-runner-config on a third-party
// host is not a GitLab project.
func (Host) Owns(repoURL string) bool {
	return domain.Locate(repoURL, "").Platform == domain.PlatformGitLab
}

// mergeRequest is what the API says of one.
type mergeRequest struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	WebURL       string `json:"web_url"`
	Draft        bool   `json:"draft"`
	State        string `json:"state"`
	TargetBranch string `json:"target_branch"`
	// A request from a fork whose branch happens to share the name is not this
	// branch's: its source is another project.
	SourceProject int    `json:"source_project_id"`
	TargetProject int    `json:"target_project_id"`
	UpdatedAt     string `json:"updated_at"`
	Author        struct {
		Username string `json:"username"`
	} `json:"author"`
}

// FindReview asks for the merge requests a branch is the source of: the open
// one when there is one, otherwise the latest that was merged or closed. It
// goes through the API rather than `glab mr list`, which prints a table meant
// for a terminal and would have to be parsed back.
func (Host) FindReview(ctx context.Context, repoURL, branch string) (*app.Review, error) {
	if branch == "" {
		return nil, nil
	}
	id, err := projectID(repoURL)
	if err != nil {
		return nil, err
	}
	out, err := glab(ctx, "api", "projects/"+id+"/merge_requests?state=all&order_by=created_at&sort=desc&per_page=10&source_branch="+url.QueryEscape(branch))
	if err != nil {
		return nil, err
	}
	var raw []mergeRequest
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("glab: read the merge requests: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	// The list is newest first. A request from a fork is not this branch's.
	var pick *mergeRequest
	for i := range raw {
		if raw[i].SourceProject != raw[i].TargetProject {
			continue
		}
		if pick == nil {
			pick = &raw[i]
		}
		if raw[i].State == "opened" {
			pick = &raw[i]
			break
		}
	}
	if pick == nil {
		return nil, nil
	}
	updated, _ := time.Parse(time.RFC3339, pick.UpdatedAt)
	state := map[string]string{"opened": "open", "merged": "merged", "closed": "closed", "locked": "closed"}[pick.State]
	if state == "open" && pick.Draft {
		state = "draft"
	}
	return &app.Review{Number: pick.IID, Title: pick.Title, URL: pick.WebURL, State: state, Draft: state == "draft",
		Base: pick.TargetBranch, Author: pick.Author.Username, Updated: updated}, nil
}

func glab(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "glab", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// As in githost: glab usually explains itself on stderr, and when it
		// says nothing at all the exit status is all there is. An error whose
		// message is empty reaches the user as a bare "error:".
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return "", fmt.Errorf("glab %s: %w", strings.Join(args, " "), err)
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}
