// Package githost creates repositories through the GitHub CLI, so credentials
// stay wherever the user already keeps them.
package githost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Host struct{}

var _ app.RepoHost = Host{}

func New() Host { return Host{} }

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
	cmd := exec.CommandContext(ctx, "gh", "repo", "create", name, visibility)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh repo create: %s", strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(url, "http") {
		return "", fmt.Errorf("gh repo create: expected the new repository's URL, got %q", url)
	}
	return url, nil
}
