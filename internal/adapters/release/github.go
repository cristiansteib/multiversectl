package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// repository is the only place the release source is named.
const repository = "cristiansteib/multiversectl"

const apiBase = "https://api.github.com"

type GitHub struct {
	client *http.Client
	base   string
}

var _ app.Releases = (*GitHub)(nil)

func New() *GitHub {
	return &GitHub{client: &http.Client{Timeout: 5 * time.Minute}, base: apiBase}
}

// Latest is the newest published release that already carries its binaries.
func (g *GitHub) Latest(ctx context.Context) (app.Release, error) {
	rels, err := g.Recent(ctx)
	if err != nil {
		return app.Release{}, err
	}
	return rels[0], nil
}

// Recent is the newest releases that already carry their binaries, newest
// first. A release is created before GoReleaser attaches them, so for a short
// while the newest one is empty and naming it would fail every update;
// checksums.txt is the last thing the pipeline needs, so its presence marks a
// finished one.
func (g *GitHub) Recent(ctx context.Context) ([]app.Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=10", g.base, repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "multiversectl")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch latest release: %s", resp.Status)
	}

	var body []struct {
		TagName    string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	var out []app.Release
	for _, r := range body {
		if r.Draft || r.Prerelease {
			continue
		}
		rel := app.Release{Version: r.TagName, Notes: r.Body, Assets: map[string]string{}}
		for _, a := range r.Assets {
			rel.Assets[a.Name] = a.URL
		}
		if _, done := rel.Assets["checksums.txt"]; done {
			out = append(out, rel)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("fetch latest release: none of the newest %d has its binaries yet", len(body))
}

// Download writes to a temp file beside dest and renames it, so a partial
// download never lands at dest, and hashes the bytes on their way through: the
// digest is of what was written, which is what the caller is about to run.
func (g *GitHub) Download(ctx context.Context, url, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "multiversectl")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("download to %s: %w", dest, err)
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("download to %s: %w", dest, err)
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, sum), resp.Body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("download to %s: %w", dest, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", fmt.Errorf("download to %s: %w", dest, err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
