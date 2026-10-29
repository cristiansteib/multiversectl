// Command docgen renders the documentation site from one layout and a page
// per topic, the way `make schema` renders the schema from the Go types: the
// chrome — topbar, sidenav, footer — is written once instead of in every page,
// so a page cannot drift from the others and a new one cannot be forgotten in
// the menu.
//
// Usage: docgen <src dir> <out dir>
//
// The source directory holds nav.yaml, layout.html and one content fragment
// per page. A fragment is the body of the page: headings, prose and code, with
// no <html> around it and no <h1>, which comes from the page's title.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// nav is the whole site: groups of pages, in the order they are read.
type nav struct {
	Groups []group `yaml:"groups"`
}

type group struct {
	Title string `yaml:"title"`
	Pages []page `yaml:"pages"`
}

type page struct {
	// Path is both the fragment's name under the source directory and the
	// page's address, without an extension: "concepts/networking".
	Path string `yaml:"path"`
	// Title heads the page and names it in the browser; Nav labels it in the
	// menu, where there is less room, and defaults to Title.
	Title string `yaml:"title"`
	Nav   string `yaml:"nav"`
	// Summary is the page's meta description and the line under its title.
	Summary string `yaml:"summary"`
	// Hero marks the one page that opens with the landing header instead of a
	// title and a table of contents.
	Hero bool `yaml:"hero"`
	// NoTOC drops the "on this page" column, for a page whose content needs
	// the width more than the reader needs the landmarks.
	NoTOC bool `yaml:"notoc"`
}

func (p page) navLabel() string {
	if p.Nav != "" {
		return p.Nav
	}
	return p.Title
}

// link is one entry of the rendered menu or pager.
type link struct {
	Label   string
	Href    string
	Current bool
}

type navGroup struct {
	Title string
	Links []link
	// Current marks the group holding the page being rendered, so the menu
	// can show where the reader is without any script.
	Current bool
}

// heading is one <h2> of a page, for its table of contents.
type heading struct {
	ID    string
	Label string
}

type pageData struct {
	// Root is the prefix that reaches the site's root from this page, so a
	// page in a subdirectory still finds the stylesheet: "" or "../".
	Root    string
	Title   string
	Summary string
	Section string
	Hero    bool
	// Wide is set by a page that dropped its table of contents, so the layout
	// can give the column that space. The hero page has no contents either,
	// but it is not asking for the width.
	Wide bool
	// HeroHTML is the landing header, which sits outside the two-column
	// layout; the one page that has one is marked in nav.yaml.
	HeroHTML    template.HTML
	Content     template.HTML
	Nav         []navGroup
	TOC         []heading
	Prev, Next  *link
	Breadcrumbs []link
	// V holds a version per asset, a hash of its content, so a page asks for
	// a new URL whenever the file changes and no browser keeps an old
	// stylesheet against a new page.
	V map[string]string
}

const heroFragment = "_hero.html"

var h2Re = regexp.MustCompile(`<h2 id="([^"]+)"[^>]*>(?:<a [^>]*>)?(.*?)(?:</a>)?</h2>`)
var tagRe = regexp.MustCompile(`<[^>]+>`)

func main() {
	if len(os.Args) != 3 {
		fail("usage: docgen <src dir> <out dir>")
	}
	if err := render(os.Args[1], os.Args[2], filepath.Join(os.Args[2], "assets")); err != nil {
		fail("%v", err)
	}
}

// assetVersions hashes each asset the layout links to.
func assetVersions(dir string) (map[string]string, error) {
	v := map[string]string{}
	for _, name := range []string{"site.css", "site.js"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		v[name] = hex.EncodeToString(sum[:4])
	}
	return v, nil
}

// render writes every page of the site from src into out. The assets are
// written by hand rather than generated, so they are read from where they are
// committed, which is out's own assets folder except under test.
func render(src, out, assets string) error {
	versions, err := assetVersions(assets)
	if err != nil {
		return err
	}

	var site nav
	raw, err := os.ReadFile(filepath.Join(src, "nav.yaml"))
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, &site); err != nil {
		return fmt.Errorf("nav.yaml: %w", err)
	}

	layout, err := template.ParseFiles(filepath.Join(src, "layout.html"))
	if err != nil {
		return err
	}

	flat, sections := flatten(site)
	if err := checkFragments(src, flat); err != nil {
		return err
	}

	for i, p := range flat {
		content, err := os.ReadFile(fragmentPath(src, p))
		if err != nil {
			return err
		}
		data := pageData{
			Root:    root(p.Path),
			Title:   p.Title,
			Summary: p.Summary,
			Section: sections[p.Path],
			Hero:    p.Hero,
			Wide:    p.NoTOC,
			Content: template.HTML(content),
			Nav:     menu(site, p),
			V:       versions,
		}
		if p.Hero {
			hero, err := os.ReadFile(filepath.Join(src, heroFragment))
			if err != nil {
				return fmt.Errorf("page %q is marked hero but %w", p.Path, err)
			}
			data.HeroHTML = template.HTML(hero)
		}
		if !p.Hero && !p.NoTOC {
			data.TOC = headings(string(content))
		}
		if !p.Hero {
			data.Breadcrumbs = []link{
				{Label: "Documentation", Href: root(p.Path) + "index.html"},
				{Label: sections[p.Path]},
			}
		}
		if i > 0 {
			data.Prev = &link{Label: flat[i-1].Title, Href: href(p.Path, flat[i-1].Path)}
		}
		if i < len(flat)-1 {
			data.Next = &link{Label: flat[i+1].Title, Href: href(p.Path, flat[i+1].Path)}
		}

		dest := filepath.Join(out, filepath.FromSlash(p.Path)+".html")
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		var page strings.Builder
		if err := layout.Execute(&page, data); err != nil {
			return fmt.Errorf("%s: %w", p.Path, err)
		}
		if err := os.WriteFile(dest, []byte(page.String()), 0o644); err != nil {
			return err
		}
	}
	// The install script is served from the site as well, so the one-liner
	// can name the project's own domain. It is copied rather than linked so
	// the published copy always matches the committed one.
	if script, err := os.ReadFile(filepath.Join(src, "..", "install.sh")); err == nil {
		if err := os.WriteFile(filepath.Join(out, "install.sh"), script, 0o755); err != nil {
			return err
		}
	}
	// GitHub Pages reads the custom domain from a CNAME file at the site's
	// root; it lives with the sources so a regenerate cannot drop it. Written
	// without a trailing newline, because changing the domain in the
	// repository's settings makes GitHub rewrite this file that way — and a
	// file that differs by one byte would fail the staleness test every time
	// someone touched the setting.
	if cname, err := os.ReadFile(filepath.Join(src, "_CNAME")); err == nil {
		domain := strings.TrimSpace(string(cname))
		if err := os.WriteFile(filepath.Join(out, "CNAME"), []byte(domain), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d pages to %s\n", len(flat), out)
	return nil
}

func flatten(site nav) ([]page, map[string]string) {
	var flat []page
	sections := map[string]string{}
	for _, g := range site.Groups {
		for _, p := range g.Pages {
			flat = append(flat, p)
			sections[p.Path] = g.Title
		}
	}
	return flat, sections
}

// checkFragments refuses to render when a page has no fragment, or a fragment
// no page: a file that is not in the menu is unreachable, which is the kind of
// mistake a generated site should not be able to keep.
func checkFragments(src string, flat []page) error {
	want := map[string]bool{}
	for _, p := range flat {
		path := fragmentPath(src, p)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("page %q is in nav.yaml but %s is missing", p.Path, path)
		}
		want[path] = true
	}
	var orphans []string
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		// layout.html and the _-prefixed partials are pieces of the chrome,
		// not pages of their own.
		if base := filepath.Base(path); base == "layout.html" || strings.HasPrefix(base, "_") || want[path] {
			return nil
		}
		orphans = append(orphans, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return fmt.Errorf("no page in nav.yaml renders: %s", strings.Join(orphans, ", "))
	}
	return nil
}

func fragmentPath(src string, p page) string {
	return filepath.Join(src, filepath.FromSlash(p.Path)+".html")
}

// root is how many directories up the site's root is from a page.
func root(path string) string {
	return strings.Repeat("../", strings.Count(path, "/"))
}

// href addresses one page from another. Pages are plain files, so the link is
// relative and works just as well from a checkout as from a web server.
func href(from, to string) string { return root(from) + to + ".html" }

func menu(site nav, current page) []navGroup {
	out := make([]navGroup, 0, len(site.Groups))
	for _, g := range site.Groups {
		rendered := navGroup{Title: g.Title}
		for _, p := range g.Pages {
			here := p.Path == current.Path
			rendered.Current = rendered.Current || here
			rendered.Links = append(rendered.Links, link{
				Label:   p.navLabel(),
				Href:    href(current.Path, p.Path),
				Current: here,
			})
		}
		out = append(out, rendered)
	}
	return out
}

// headings reads a page's <h2>s for its table of contents. The fragments are
// written by hand, so this is a scan rather than a parse: every heading that
// should appear carries an id, and one without an id is deliberately not a
// landmark.
func headings(content string) []heading {
	var out []heading
	for _, m := range h2Re.FindAllStringSubmatch(content, -1) {
		label := strings.TrimSpace(tagRe.ReplaceAllString(m[2], ""))
		if label == "" {
			continue
		}
		out = append(out, heading{ID: m[1], Label: label})
	}
	return out
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "docgen: "+format+"\n", a...)
	os.Exit(1)
}
