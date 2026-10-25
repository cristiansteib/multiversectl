package githost

import "testing"

func TestParseRepos(t *testing.T) {
	got, err := parseRepos(`[{"nameWithOwner":"acme/api","url":"https://github.com/acme/api","description":"the api","isPrivate":true},{"nameWithOwner":"acme/web","url":"https://github.com/acme/web","description":"","isPrivate":false}]`)
	if err != nil || len(got) != 2 || got[0].Name != "acme/api" || !got[0].Private || got[1].Private || got[0].Description != "the api" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := parseRepos("not json"); err == nil {
		t.Error("garbage must be an error")
	}
}
