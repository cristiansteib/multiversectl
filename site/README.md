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
| `src/data/nav.ts` | **The menu and the page order.** Ported from `docsrc/nav.yaml`: same groups, same order, same titles, nav labels, summaries and `hero`/`notoc` flags. The sidenav, the top bar's section links, the breadcrumb and previous/next all derive from it. |
| `src/layouts/Page.astro` | The chrome of an ordinary page: top bar, menu, breadcrumb, h1, lede, content, pager, contents column, footer. |
| `src/layouts/Home.astro` | The hero variant: landing header, no menu, no breadcrumb, no contents column. |
| `src/components/` | The pieces of the chrome: `BaseHead`, `TopBar`, `SideNav`, `Breadcrumbs`, `Toc`, `Pager`, `Hero`, `Footer`, `Scripts`. |
| `src/lib/headings.ts` | Reads a page's `<h2 id>`s for its contents column. |
| `src/styles/site.css` | The design system, ported byte for byte from `docs/assets/site.css`. Every class name is unchanged. |
| `src/scripts/` | `site.ts` and the modules it imports: the copy buttons, the install tabs, the sidenav drawer, the theme button, the comparison notes, the contents column's scroll-spy. One bundle, shared by both layouts. |
| `src/scripts/pages/` | A page's own behaviour, one module per page. `index.ts` is the agent animation. |
| `src/pages/` | One `.astro` file per page, at the path it is published under. |
| `public/` | Served at the site root. `brands/` is committed; `install.sh` and `assets/img/` are copied in by `npm run sync` and gitignored. |
| `scripts/port-fragment.mjs` | Turns a `docsrc/` fragment into a page here. See below. |
| `scripts/sync-public.mjs` | Copies `install.sh` and `docs/assets/img` into `public/`, so each has one source of truth. |

## URLs: unchanged, and no redirects needed

`build.format: 'file'`, so a page is published at exactly the path it had
before: `/index.html`, `/docs.html`, `/start/install.html`,
`/concepts/networking.html`. **There is no old → new mapping, because nothing
moved.** The relative links inside the content (`../tasks/server.html`) keep
resolving for the same reason, which is why the content can be pasted in as it
is.

One thing to settle in hosting: `firebase.json` currently sets
`"cleanUrls": true`, which makes Firebase redirect `/start/install.html` to
`/start/install` (301) and serve the file there. Nothing breaks — no URL 404s
— but every published link then costs a redirect hop and the canonical URL
changes. **Set `"cleanUrls": false`** to keep the published URLs byte for byte
what they have always been. If you would rather have clean URLs, that is a
deliberate change of address for 25 pages: say so, and the chrome's links in
`src/data/nav.ts` (`href()`) and the relative links in the content both need
rewriting.

## The contents column ("On this page")

`Page.astro` renders the page's content to a string first and scans that string
for `<h2 id="...">`, exactly as `docgen` scanned the fragments. So:

- **A heading is in the contents column when, and only when, it carries an id.**
  Leaving the id off is how a heading is deliberately not a landmark — the same
  rule `docsrc/README.md` states.
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
   Nothing else decides the menu, the breadcrumb or the pager.
2. Port the content:

   ```sh
   node scripts/port-fragment.mjs concepts/networking
   node scripts/port-fragment.mjs index --layout home   # only the home
   ```

   It writes `src/pages/<path>.astro` and refuses to overwrite a page that is
   already there (`--force` overrides). The content is carried over verbatim
   bar three mechanical changes, because an `.astro` file is not quite HTML:

   - `{` and `}` become `&#123;` / `&#125;`. A brace starts an expression in an
     Astro template; the entities render identically. Braces inside `<style>`
     and `<script>` are left alone.
   - `<style>` becomes `<style is:global>`. Astro scopes styles to the
     component by default, and these select ids and classes in hand-written
     SVG.
   - every inline `<script>` moves to `src/scripts/pages/<slug>.ts`, which the
     page imports. A `<script src="https://cdnjs…gsap…">` tag is dropped and
     `import gsap from 'gsap'` put at the top of that module instead.

3. `npm run check && npm run build`.

A page written by hand instead looks like this, and that is all it needs:

```astro
---
import Page from '../layouts/Page.astro';
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

- **Content.** 25 pages are in `src/data/nav.ts`; three are ported
  (`index`, `docs`, `start/install`). The rest are one
  `scripts/port-fragment.mjs` run each. `docsrc/start/lifecycle.html` also
  loads GSAP from a CDN and is `notoc`; the tool handles both.
- **Islands.** Drop a component anywhere in a page's content — the content is
  ordinary Astro markup, not an opaque string. Page behaviour belongs in
  `src/scripts/pages/<slug>.ts`; the ported modules carry `@ts-nocheck` because
  they are the old hand-written JS as it was, and dropping that line is the
  measure of having rewritten one properly.
- **SEO.** `site` is set to `https://multiversectl.dev`, so `@astrojs/sitemap`
  and canonical URLs need no further configuration. There is no `robots.txt`,
  no sitemap and no Open Graph or JSON-LD yet; `BaseHead.astro` is where the
  tags belong, and it is the only place to add them. Note that with
  `build.format: 'file'` a canonical URL ends in `.html` unless the hosting
  decision above changes.
- **Deployment.** GitHub Pages, from `main:/docs`. Two files in `public/` are
  what make that work and are easy to lose: `CNAME`, which names the domain,
  and `.nojekyll` — without it Pages applies Jekyll's rule that paths beginning
  with an underscore are private, refuses to serve `_astro/`, and every
  stylesheet and script on the site 404s. `make docs` checks both exist.
  `firebase.json` at the repository root is a spare target for the same
  `dist/`, kept for previewing a build on a real URL; it is not the live site.

[Astro]: https://astro.build
