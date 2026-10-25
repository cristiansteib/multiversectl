# Documentation sources

`docs/` is generated. Edit the files here and run `make docs`.

| File | What it is |
|---|---|
| `nav.yaml` | The menu and the page order. A page listed here needs a fragment of the same path; a fragment no page renders is an error, because nothing would link to it. |
| `layout.html` | The chrome every page shares: topbar, menu, breadcrumb, table of contents, previous/next, footer. |
| `_hero.html` | The landing header, used by the one page marked `hero: true`. |
| `<path>.html` | One page's content: sections, prose and code, with no `<h1>` — the title comes from `nav.yaml`. |

A page's `<h2 id="...">` headings become its table of contents, so give every
section heading an id and leave one off deliberately when it should not be a
landmark.

Links between pages are relative and written as they appear on disk
(`../concepts/networking.html`), so the site works from a checkout as well as
from a web server. `go test ./internal/tools/docgen/` fails when `docs/` is
stale, so CI catches a forgotten `make docs`.
