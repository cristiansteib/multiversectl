package gitlabhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGlab puts a glab on the PATH that answers each API call with a canned
// reply, so the adapter's parsing is checked without an account.
func fakeGlab(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  "api user") echo '{"username":"me"}' ;;
  "api groups?min_access_level=30&per_page=100") echo '[{"full_path":"acme"},{"full_path":"acme/sub"}]' ;;
  "api projects?membership=true&simple=true&per_page=100 --paginate")
    echo '[{"path_with_namespace":"acme/api","http_url_to_repo":"https://gitlab.example.test/acme/api.git","description":"the api","visibility":"private"}]'
    echo '[{"path_with_namespace":"acme/sub/web","http_url_to_repo":"https://gitlab.example.test/acme/sub/web.git","visibility":"public"},{"path_with_namespace":"me/notes","http_url_to_repo":"https://gitlab.example.test/me/notes.git","visibility":"private"}]' ;;
  "api namespaces/acme") echo '{"id":42}' ;;
  "api projects?membership=true&simple=true&per_page=100&topic=multiversectl-root --paginate")
    echo '[{"path_with_namespace":"me/multiverse-index","http_url_to_repo":"https://gitlab.example.test/me/multiverse-index.git","visibility":"private"}]' ;;
  "api projects/me%2Fmultiverse-index") echo '{"topics":["go","cli"]}' ;;
  "api projects/me%2Fmultiverse-index -X PUT -f topics[]=go -f topics[]=cli -f topics[]=multiversectl-root") echo '{}' ;;
  "api projects -X POST -f name=multiverse -f path=multiverse -f visibility=private -f namespace_id=42")
    echo '{"http_url_to_repo":"https://gitlab.example.test/acme/multiverse.git"}' ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAccountsAreTheUserThenTheirGroups(t *testing.T) {
	fakeGlab(t)
	got, err := Host{}.Accounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "me" || got[0].Org || got[1].Name != "acme" || !got[1].Org || got[2].Name != "acme/sub" {
		t.Errorf("accounts = %+v", got)
	}
}

func TestReposOfAnAccountIncludeItsSubgroupsAndNobodyElses(t *testing.T) {
	fakeGlab(t)
	got, err := Host{}.Repos(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "acme/api" || !got[0].Private || got[1].Name != "acme/sub/web" || got[1].Private {
		t.Errorf("repos = %+v: want both pages, acme's only", got)
	}
}

func TestCreateFindsTheNamespaceAndReturnsTheCloneURL(t *testing.T) {
	fakeGlab(t)
	url, err := Host{}.Create(context.Background(), "acme/multiverse", true)
	if err != nil || url != "https://gitlab.example.test/acme/multiverse.git" {
		t.Fatalf("url %q, err %v", url, err)
	}
	if _, err := (Host{}).Create(context.Background(), "nowhere/x", true); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("a failing glab call must surface its words, got %v", err)
	}
}

func TestRootsAreFoundByTopicAndTaggedWithoutLosingTopics(t *testing.T) {
	fakeGlab(t)
	got, err := Host{}.ReposWithTopic(context.Background(), "multiversectl-root")
	if err != nil || len(got) != 1 || got[0].Name != "me/multiverse-index" || !got[0].Private {
		t.Fatalf("found %+v, %v", got, err)
	}
	// Setting topics replaces the list, so the ones it had must be sent back too;
	// the stand-in only accepts exactly that request.
	if err := (Host{}).AddTopic(context.Background(), "https://gitlab.example.test/me/multiverse-index.git", "multiversectl-root"); err != nil {
		t.Fatal(err)
	}
}

// The API addresses a project by its escaped path, which has to come from the
// URL's platform and path and not from trimming the text of an https URL.
func TestProjectIDIsThePathWhateverTheURLLooksLike(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://gitlab.com/me/index.git", "me%2Findex"},
		{"https://gitlab.com/me/index", "me%2Findex"},
		{"git@gitlab.com:me/index.git", "me%2Findex"},
		{"ssh://git@gitlab.com/me/index.git", "me%2Findex"},
		{"git@gitlab.com:grp/sub/index.git", "grp%2Fsub%2Findex"},
		{"https://gitlab.example.test/grp/index.git", "grp%2Findex"},
	} {
		got, err := projectID(tc.url)
		if err != nil || got != tc.want {
			t.Errorf("projectID(%q) = %q, %v; want %q", tc.url, got, err, tc.want)
		}
	}
	for _, bad := range []string{"https://github.com/me/index.git", "git@git.acme.test:infra/gitlab-runner-config.git", ""} {
		if _, err := projectID(bad); err == nil {
			t.Errorf("projectID(%q) must refuse a URL that is not a GitLab project", bad)
		}
	}
}

func TestOwnsAsksThePlatformNotTheText(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://gitlab.com/me/index.git", true},
		{"git@gitlab.com:me/index.git", true},
		{"https://gitlab.example.test/grp/index.git", true},
		{"https://github.com/acme/gitlab-exporter.git", false},
		{"https://git.acme.test/infra/gitlab-runner-config.git", false},
	} {
		if got := (Host{}).Owns(tc.url); got != tc.want {
			t.Errorf("Owns(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
