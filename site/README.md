# The multiversectl site

The static site for <https://multiversectl.dev>, built with [Astro]. It
replaced `internal/tools/docgen`, which rendered `docs/` from a folder of HTML
fragments in `docsrc/`; both are gone.

`make docs` builds this project and copies `dist/` over `docs/`, which is the
folder GitHub Pages publishes (its source is `main:/docs`). So `docs/` is build
output that happens to be committed: edit this project, never `docs/`. CI
rebuilds and fails if the two have drifted.

```sh
npm install
npm run dev      # http://localhost:4321, hot reload
npm run build    # static HTML into dist/
npm run preview  # serve dist/ exactly as it will be published
npm run check    # astro check: TypeScript and template diagnostics, 0 errors expected
npm run sync     # copy install.sh and the screenshots into public/ (build does this)
```

`npm run build` writes `dist/`, which is what `firebase.json` publishes.

## Layout

| Path | What it is |
|---|---|
| `src/data/nav.ts` | **The menu, the page order and every address.** Groups, titles, nav labels, summaries, descriptions and the `hero`/`notoc` flags. The sidenav, the top bar's section links, the breadcrumb, previous/next, the hero's buttons and the redirect stubs all derive from it. |
| `src/layouts/Page.astro` | The chrome of an ordinary page: top bar, menu, breadcrumb, h1, lede, content, pager, contents column, footer. |
| `src/layouts/Home.astro` | The hero variant: landing header, no menu, no breadcrumb, no contents column. |
| `src/components/` | **The chrome**: `BaseHead`, `TopBar`, `SideNav`, `Breadcrumbs`, `Toc`, `Pager`, `Hero`, `Footer`, `Scripts`. **The content**: `Terminal`, `Figure`, `Shot`, `Note`, `Cards`/`Card` — see below. |
| `src/lib/headings.ts` | Reads a page's `<h2 id>`s for its contents column. |
| `src/styles/site.css` | The design system, ported byte for byte from `docs/assets/site.css`. Every class name is unchanged. |
| `src/scripts/` | `site.ts` and the modules it imports: the copy buttons, the install tabs, the sidenav drawer, the theme button, the comparison notes, the contents column's scroll-spy. One bundle, shared by both layouts. |
| `src/scripts/pages/` | A page's own behaviour, one module per page. `index.ts` is the agent animation. |
| `src/pages/` | One `.astro` file per page, at the path it is published under. |
| `public/` | Served at the site root. `brands/` is committed; `install.sh` and `assets/img/` are copied in by `npm run sync` and gitignored. |
| `scripts/sync-public.mjs` | Copies the repository's `install.sh` into `public/`, so there is one of it. |
| `scripts/redirects.mjs` | Writes a stub at every address the site used to publish, from `src/data/nav.ts`. See **URLs**. |
| `scripts/verify-site.mjs` | Checks a built or deployed site: every page answers, every link and anchor resolves, the metadata is complete. CI runs it. |

## The content components

A page's prose is ordinary markup, but the five patterns that repeat are
components, so there is one definition of each instead of one per use.

| Component | Replaces | Uses |
|---|---|---|
| `Terminal` | `<div class="code"><pre>…</pre></div>` | 101 |
| `Figure` | `<figure class="fig wide">` + `<figcaption>` | 7 |
| `Card` in `Cards` | `<a class="card">` with its `<strong>`/`<span>` | 15 in 3 |
| `Shot` | `<figure class="shot">` with its `<picture>` | 3 |
| `Note` | `<div class="note">` | 3 |

```astro
<Terminal>
<span class="p">$ </span>multiversectl universe ls
<span class="k">universe 3</span>  ·  acme-u3  ·  6 running
</Terminal>

<Figure id="ag">
  <svg viewBox="0 0 720 520" role="img" aria-labelledby="pt pd">
    <title id="pt">The claim this picture makes</title>
    <desc id="pd">What someone who cannot see it needs told.</desc>
    …
  </svg>
  <Fragment slot="caption">The same claim, visibly, with <a href="x.html">a link</a>.</Fragment>
</Figure>
```

Two of them **fail the build** rather than let a page ship something broken,
which is the point of having them at all:

- **`Figure`** requires `role="img"` and a label on the SVG, a `<title>` when
  the label is an `aria-labelledby`, and a non-empty caption. A diagram without
  them is announced to a screen reader as nothing whatsoever — easy to leave
  out, impossible to see you have.
- **`Shot`** requires `width`, `height` and an `alt` that says what the picture
  shows. Without the dimensions the page reflows when the image arrives.

`verify-site.mjs` checks the same things on the built HTML, so the component is
the early, specific failure and the verifier is the backstop.

### What is deliberately not a component

- **The Copy button.** `Terminal` does not render one. `src/scripts/copy.ts`
  adds a button to every `.code` at runtime, so a reader with no JavaScript is
  never shown a button that cannot work. The markup and the behaviour are
  owned in those two places; keep them in step.
- **The walkthrough's shell and scene** on `start/lifecycle`. The three `<pre>`
  elements there are written by a script, and the `<figure>` around the scene
  has page-specific CSS. One use, not a pattern.

### The vocabulary inside a `Terminal`

Four classes, and nothing else in a code block is coloured:

| Class | Means |
|---|---|
| `p` | the prompt. Unselectable, and the copy button strips it, so the clipboard gets the command without the `$ `. |
| `c` | a comment. |
| `k` | the thing to notice in the output. |
| `s` | a quoted string. |

## URLs

The landing page is the site root. Everything else lives under `/docs/` and
every address is extensionless, with a trailing slash:

| Page | URL |
|---|---|
| landing | `/` |
| documentation index | `/docs/` |
| a page | `/docs/start/install/`, `/docs/concepts/networking/` |

`build.format: 'directory'` writes each page as `<path>/index.html`, which is
what lets GitHub Pages serve a path with no extension, and `trailingSlash:
'always'` matches. Ask for `/docs/start/install` without the slash and Pages
answers 301 to the slashed form, so the canonical, the sitemap and every link
on the site all name the slashed form and nothing pays for the hop.

**One rule decides every link in the chrome:** `href()` in `src/data/nav.ts`.
The sidenav, the top bar, the breadcrumb, the pager and the hero's buttons all
go through it. The canonical and `og:url` are built the same way in
`BaseHead.astro`, which cannot use `href()` only because the 404 is not in the
nav. Change the shape of an address in those two places and nowhere else.

**Links inside the content are absolute** (`/docs/tasks/server/`), not
relative. Under `build.format: 'file'` a page at `/concepts/networking.html`
sat in `/concepts/`, so `../tasks/server.html` resolved; under `directory` the
page is at `/docs/concepts/networking/`, so the same relative link would
resolve to `/docs/concepts/tasks/server/` instead. Absolute paths do not care
how deep a page is.

### The addresses this replaces

The site used to publish `/start/install.html`, `/concepts/networking.html` and
22 more, and those are out there — indexed, linked, bookmarked. GitHub Pages
serves static files and cannot answer with a 301, so `scripts/redirects.mjs`
writes a stub at every one of them, generated from `src/data/nav.ts` on every
build (`npm run sync`) into `public/` and gitignored, so the nav is their only
source and a stale one cannot linger. Each stub names its replacement three
ways: a canonical link for a crawler, a meta refresh for a browser, and a
visible link for anyone with neither.

They are stubs and not pages: nothing on the site links to one, they are
`noindex, follow`, and they are not in the sitemap. `verify-site.mjs` checks
every one of them still lands a reader on the page that replaced it.

The landing page has no stub — it did not move, and `/index.html` is still the
file the build writes at the root.

## The contents column ("On this page")

`Page.astro` renders the page's content to a string first and scans that string
for `<h2 id="...">`, exactly as `docgen` scanned the fragments. So:

- **A heading is in the contents column when, and only when, it carries an id.**
  Leaving the id off is how a heading is deliberately not a landmark — the same
  rule the old generator used.
- There is nothing to keep in step: a page declares its headings by having
  them. Do not list them anywhere.
- A page that needs to override the list can pass `headings={[{id, label}]}` to
  `Page`, and a page marked `notoc` in `src/data/nav.ts` has no column at all
  and gets the width instead (`class="wrap layout wide"`).

The scan lives in `src/lib/headings.ts` and expects the shape every page
already uses, on one line:

```html
<h2 id="binary"><a href="#binary">Ready-made binaries</a></h2>
```

## Adding a page

1. Add it to `src/data/nav.ts`, in the group and the position it belongs in.
   Nothing else decides the menu, the breadcrumb, the pager or its address.
2. Write `src/pages/docs/<path>.astro`:

   ```astro
   ---
   import Page from '../../../layouts/Page.astro';
   ---

   <Page path="concepts/networking">
     <section id="entrypoints">
       <h2 id="entrypoints"><a href="#entrypoints">Entrypoints</a></h2>
       …
     </section>
   </Page>
   ```

   `Page` takes the title, the lede, the breadcrumb, the current menu entry and
   previous/next from `path`. `title`, `summary`, `section` and `notoc` exist as
   props too, but only as overrides: `src/data/nav.ts` is the source.

   Links to other pages are absolute (`/docs/tasks/server/`), and the patterns
   that repeat are the components above, not hand-written markup.
3. `npm run check && npm run build`, then `node scripts/verify-site.mjs` against
   the result.

A page added here gets a redirect stub only if it had an old address; a new
page never does.

## No CDN

The only external request a page makes is the IBM Plex stylesheet from Google
Fonts, which the old site made too. GSAP is an npm dependency, bundled, and
only into the pages that animate something — `dist/_astro/index.astro_…js` is
loaded by the home and by nothing else. If the fonts should be self-hosted as
well, `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` drop in
without touching `site.css`.

## Themes, and the flash that isn't

Light is the default; `:root[data-theme=dark]` is the opt-in, as in the ported
CSS. `BaseHead.astro` carries an `is:inline` script that applies a stored
`dark` choice before the first paint — it must stay inline and unbundled to run
that early. `src/scripts/theme.ts` then keeps the button, `meta[name=theme-color]`
and the light-version screenshots in step with it.

Each script finds its own elements and does nothing when they are not there, so
the home — which has no sidenav and no contents column — runs the same bundle.
(The old `site.js` was one block that bound the drawer unconditionally, so on
the home it threw at the first missing element and the theme button stayed
hidden. It no longer can.)

## Notes for whoever builds on this

- **Content.** 25 pages, all listed in `src/data/nav.ts`, which is the only
  place that decides the menu, the order, the breadcrumb, the pager and each
  page's address.
- **Islands.** Drop a component anywhere in a page's content — the content is
  ordinary Astro markup, not an opaque string. Page behaviour belongs in
  `src/scripts/pages/<slug>.ts`; the modules carried over from the old
  hand-written JS still have `@ts-nocheck`, and dropping that line is the
  measure of having rewritten one properly.
- **SEO.** `site` is `https://multiversectl.dev`, so `@astrojs/sitemap` and the
  canonical URLs follow from it. `BaseHead.astro` holds every tag — canonical,
  Open Graph, Twitter card, JSON-LD — and is the only place to add one.
  `public/robots.txt` points at the sitemap. `scripts/verify-site.mjs` is what
  keeps all of it honest, and CI fails on it.
- **Widths.** Two containers, both in `src/styles/site.css`. `.wrap` is the
  documentation's, and it is deliberately wide — the menu and the contents
  column are navigation, so the middle column gets what is left, which is what
  the code blocks and diagrams need. `.landing .wrap` caps the landing page
  instead, and `.landing main > section` caps each section just past the widest
  diagram, so the heading, the prose and the picture share one left edge
  however wide the window is.
- **Deployment.** GitHub Pages, from `main:/docs`. Two files in `public/` are
  what make that work and are easy to lose: `CNAME`, which names the domain,
  and `.nojekyll` — without it Pages applies Jekyll's rule that paths beginning
  with an underscore are private, refuses to serve `_astro/`, and every
  stylesheet and script on the site 404s. `make docs` checks both exist.
  `firebase.json` at the repository root is a spare target for the same
  `dist/`, kept for previewing a build on a real URL; it is not the live site.

[Astro]: https://astro.build
