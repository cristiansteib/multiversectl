package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Git struct{}

func New() *Git { return &Git{} }

func (Git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (g Git) Clone(ctx context.Context, url, dest string) error {
	_, err := g.run(ctx, "clone", url, dest)
	return err
}

func (g Git) Fetch(ctx context.Context, repoDir string) error {
	_, err := g.run(ctx, "-C", repoDir, "fetch", "--quiet", "origin")
	return err
}

func (g Git) AddWorktree(ctx context.Context, repoDir, dest, branch, base string) error {
	args := []string{"-C", repoDir, "worktree", "add"}
	switch {
	case base == "":
		args = append(args, dest, branch)
	case strings.HasPrefix(base, "origin/"):
		args = append(args, "--track", "-b", branch, dest, base)
	default:
		args = append(args, "-b", branch, dest, base)
	}
	_, err := g.run(ctx, args...)
	return err
}

func (g Git) RemoveWorktree(ctx context.Context, repoDir, dest string) error {
	_, err := g.run(ctx, "-C", repoDir, "worktree", "remove", "--force", dest)
	return err
}

func (g Git) CurrentBranch(ctx context.Context, dir string) (string, error) {
	return g.run(ctx, "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// show-ref exits non-zero when the ref is missing; that is an answer.
func (g Git) refPresent(ctx context.Context, repoDir, full string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show-ref", "--verify", "--quiet", full)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, fmt.Errorf("git show-ref: %w", err)
}

func (g Git) RefExists(ctx context.Context, repoDir, ref string) (app.RefKind, error) {
	if ok, err := g.refPresent(ctx, repoDir, "refs/heads/"+ref); err != nil {
		return app.RefNone, err
	} else if ok {
		return app.RefLocal, nil
	}
	if ok, err := g.refPresent(ctx, repoDir, "refs/remotes/origin/"+ref); err != nil {
		return app.RefNone, err
	} else if ok {
		return app.RefRemote, nil
	}
	return app.RefNone, nil
}

func (g Git) DefaultBranch(ctx context.Context, repoDir string) (string, error) {
	if out, err := g.run(ctx, "-C", repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return strings.TrimPrefix(out, "origin/"), nil
	}
	for _, name := range []string{"main", "master"} {
		ok, err := g.refPresent(ctx, repoDir, "refs/remotes/origin/"+name)
		if err != nil {
			return "", err
		}
		if ok {
			return name, nil
		}
	}
	return "", errors.New("cannot determine the default branch of origin")
}

// Checkout prefers origin/<ref> so a branch name lands on what was last
// fetched and not on a stale local branch from the first clone.
func (g Git) Checkout(ctx context.Context, repoDir, ref string) error {
	target := ref
	if _, err := g.run(ctx, "-C", repoDir, "rev-parse", "--verify", "--quiet", "origin/"+ref); err == nil {
		target = "origin/" + ref
	}
	_, err := g.run(ctx, "-C", repoDir, "checkout", "--quiet", "--detach", target)
	return err
}
