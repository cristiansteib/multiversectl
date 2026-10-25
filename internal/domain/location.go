package domain

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Platform is where a repository, or a file, lives.
type Platform string

const (
	PlatformGitHub Platform = "github"
	PlatformGitLab Platform = "gitlab"
	PlatformGit    Platform = "git" // any other git host
	PlatformFile   Platform = "file"
)

// Location says where something is in the terms a person uses: not a URL, which
// is how it was written, but the platform and the path on it. Three catalogs in
// one repository and three repositories on three services read the same as URLs;
// they do not read the same here.
type Location struct {
	Platform Platform `json:"platform"`
	// Host is empty for github.com and gitlab.com, the two everyone means.
	Host string `json:"host,omitempty"`
	// Path is owner/repo (or group/sub/repo) on a service, a filesystem path for a file.
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
	// URL is as it was written, for a title or a copy.
	URL string `json:"url,omitempty"`
}

var scpLike = regexp.MustCompile(`^[\w.-]+@([\w.-]+):(.+)$`)

// Locate reads a repository as it was written: an https or ssh URL, an scp-style
// address, a file:// URL, a filesystem path, or owner/repo, which means GitHub.
// Empty is the zero Location.
func Locate(repo, ref string) Location {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return Location{}
	}
	l := Location{URL: repo, Ref: ref}
	switch {
	case strings.HasPrefix(repo, "file://"):
		l.Platform, l.Path = PlatformFile, strings.TrimPrefix(repo, "file://")
		return l
	case filepath.IsAbs(repo) || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") || strings.HasPrefix(repo, "~"):
		l.Platform, l.Path = PlatformFile, strings.TrimSuffix(repo, "/")
		return l
	}

	var host, path string
	if m := scpLike.FindStringSubmatch(repo); m != nil {
		host, path = m[1], m[2]
	} else if i := strings.Index(repo, "://"); i >= 0 {
		rest := repo[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && at < strings.Index(rest+"/", "/") {
			rest = rest[at+1:]
		}
		host, path, _ = strings.Cut(rest, "/")
		if colon := strings.Index(host, ":"); colon >= 0 {
			host = host[:colon] // a port is not part of who it is
		}
	} else {
		// owner/repo, the way it is said out loud.
		l.Platform, l.Path = PlatformGitHub, clean(repo)
		return l
	}
	l.Path = clean(path)
	switch h := strings.ToLower(host); {
	case h == "github.com":
		l.Platform = PlatformGitHub
	case h == "gitlab.com":
		l.Platform = PlatformGitLab
	case strings.Contains(h, "github"):
		l.Platform, l.Host = PlatformGitHub, host
	case strings.Contains(h, "gitlab"):
		l.Platform, l.Host = PlatformGitLab, host
	default:
		l.Platform, l.Host = PlatformGit, host
	}
	return l
}

// LocateFile is the Location of a file on this machine.
func LocateFile(path string) Location {
	return Location{Platform: PlatformFile, Path: path, URL: path}
}

func clean(p string) string {
	p = strings.Trim(p, "/")
	return strings.TrimSuffix(p, ".git")
}

// Short is the notation used wherever a location is shown: github:owner/repo,
// gitlab:group/repo, git:host/path for any other host, file:~/path for a file,
// each with @ref when there is one. home, when given, is written as ~ in a path.
func (l Location) Short(home string) string {
	if l.Platform == "" {
		return ""
	}
	var body string
	switch l.Platform {
	case PlatformFile:
		body = l.Path
		if home != "" && (body == home || strings.HasPrefix(body, home+string(filepath.Separator))) {
			body = "~" + strings.TrimPrefix(body, home)
		}
	case PlatformGit:
		body = l.Host + "/" + l.Path
	default:
		body = l.Path
		if l.Host != "" {
			body = l.Host + "/" + body
		}
	}
	s := string(l.Platform) + ":" + body
	if l.Ref != "" {
		s += "@" + l.Ref
	}
	return s
}

// Location is where a catalog or project node lives: a repository on a
// service, or a file on this machine.
func (n Node) Location() Location {
	switch {
	case n.Source.Repo != "":
		return Locate(n.Source.Repo, n.Source.Ref)
	case n.Source.Path != "" && filepath.IsAbs(n.Source.Path):
		return LocateFile(n.Source.Path)
	case n.File != "":
		return LocateFile(n.File)
	}
	return Location{}
}

// FileWithin is the file inside a repository that a node is, when it is not the
// one that would be assumed: it is what tells two catalogs in one repository apart.
func (n Node) FileWithin() string {
	p := n.Source.Path
	if n.Source.Repo == "" || p == "" || p == "catalog.yaml" || p == "multiverse.yaml" {
		return ""
	}
	return p
}
