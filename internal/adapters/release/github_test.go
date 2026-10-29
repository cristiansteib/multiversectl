package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The newest release is empty while the pipeline is still attaching its
// binaries; the one before it is what an update should name.
func TestLatestSkipsAReleaseStillBeingPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"tag_name":"v2.0.0","assets":[]},
			{"tag_name":"v1.9.0","prerelease":true,"assets":[{"name":"checksums.txt","browser_download_url":"u/pre"}]},
			{"tag_name":"v1.8.0","assets":[{"name":"multiversectl_linux_amd64","browser_download_url":"u/bin"},{"name":"checksums.txt","browser_download_url":"u/sum"}]}
		]`))
	}))
	defer srv.Close()

	g := New()
	g.base = srv.URL
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v1.8.0" || rel.Assets["multiversectl_linux_amd64"] != "u/bin" {
		t.Fatalf("got %+v", rel)
	}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"tag_name":"v2.0.0","assets":[]}]`))
	})
	if _, err := g.Latest(context.Background()); err == nil {
		t.Fatal("want an error when no release is complete")
	}
}

// A tag published ahead of its binaries is reported as on its way, so that an
// update does not call the older release "the latest" without saying so.
func TestLatestSaysWhichNewerVersionIsOnItsWay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"tag_name":"v2.0.0","assets":[]},
			{"tag_name":"v1.8.0","assets":[{"name":"checksums.txt","browser_download_url":"u/sum"}]}
		]`))
	}))
	defer srv.Close()
	g := New()
	g.base = srv.URL
	rel, err := g.Latest(context.Background())
	if err != nil || rel.Version != "v1.8.0" || rel.Pending != "v2.0.0" {
		t.Fatalf("rel = %+v, err = %v", rel, err)
	}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"tag_name":"v2.0.0","assets":[]}]`))
	})
	if _, err := g.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "v2.0.0 is being published") {
		t.Fatalf("err = %v", err)
	}
}
