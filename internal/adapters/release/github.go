package release

import (
	"context"
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

func (g *GitHub) Latest(ctx context.Context) (app.Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", g.base, repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return app.Release{}, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "multiversectl")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return app.Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return app.Release{}, fmt.Errorf("fetch latest release: %s", resp.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return app.Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	rel := app.Release{Version: body.TagName, Assets: map[string]string{}}
	for _, a := range body.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Download writes to a temp file beside dest and renames it, so a partial
// download never lands at dest.
func (g *GitHub) Download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "multiversectl")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("download to %s: %w", dest, err)
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return fmt.Errorf("download to %s: %w", dest, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("download to %s: %w", dest, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("download to %s: %w", dest, err)
	}
	return nil
}
