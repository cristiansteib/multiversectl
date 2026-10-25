package domain

import "testing"

func TestLocationsAreWrittenByPlatformNotByURL(t *testing.T) {
	for _, tt := range []struct {
		repo, ref, home string
		platform        Platform
		short           string
	}{
		{"https://github.com/acme/api.git", "", "", PlatformGitHub, "github:acme/api"},
		{"git@github.com:acme/api.git", "", "", PlatformGitHub, "github:acme/api"},
		{"ssh://git@github.com/acme/api", "main", "", PlatformGitHub, "github:acme/api@main"},
		{"acme/api", "", "", PlatformGitHub, "github:acme/api"},
		{"https://gitlab.com/acme/team/api.git", "v1", "", PlatformGitLab, "gitlab:acme/team/api@v1"},
		{"git@gitlab.com:acme/api.git", "", "", PlatformGitLab, "gitlab:acme/api"},
		{"https://gitlab.example.test/acme/api.git", "", "", PlatformGitLab, "gitlab:gitlab.example.test/acme/api"},
		{"https://github.example.test:8443/acme/api", "", "", PlatformGitHub, "github:github.example.test/acme/api"},
		{"https://git.example.test/acme/api.git", "", "", PlatformGit, "git:git.example.test/acme/api"},
		{"ssh://git@git.example.test:2222/acme/api.git", "", "", PlatformGit, "git:git.example.test/acme/api"},
		{"file:///home/me/code/api", "", "/home/me", PlatformFile, "file:~/code/api"},
		{"/home/me/code/api/", "", "/home/me", PlatformFile, "file:~/code/api"},
		{"/srv/git/api.git", "", "/home/me", PlatformFile, "file:/srv/git/api.git"},
		{"/home/mea/x", "", "/home/me", PlatformFile, "file:/home/mea/x"}, // a prefix of the name is not the home
	} {
		l := Locate(tt.repo, tt.ref)
		if l.Platform != tt.platform || l.Short(tt.home) != tt.short {
			t.Errorf("Locate(%q) = %s %q, want %s %q", tt.repo, l.Platform, l.Short(tt.home), tt.platform, tt.short)
		}
		if l.URL != tt.repo {
			t.Errorf("the URL as written is kept: %q", l.URL)
		}
	}
	if l := Locate("", ""); l.Platform != "" || l.Short("") != "" {
		t.Errorf("nothing is nothing: %+v", l)
	}
	if got := LocateFile("/home/me/index/catalog.yaml").Short("/home/me"); got != "file:~/index/catalog.yaml" {
		t.Errorf("LocateFile = %q", got)
	}
}
