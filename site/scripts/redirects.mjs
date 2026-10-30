// Write a stub at every address this site used to publish.
//
// The documentation moved from /start/install.html to /docs/start/install/ and
// those old addresses are already out there — indexed, linked, bookmarked.
// GitHub Pages serves static files and cannot answer with a 301, so each old
// address gets a small page that names its replacement three ways: a canonical
// link (what a crawler follows), a meta refresh (what a browser follows), and
// a visible link (what someone with neither ends up clicking).
//
// These go in public/, not src/pages/: a page under build.format 'directory'
// would be written as <path>/index.html, and what has to exist here is
// literally <path>.html. Nothing on the site links to one.
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { pages, href } from '../src/data/nav.ts';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const ORIGIN = 'https://multiversectl.dev';

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');

const stub = (to, title) => `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Moved to ${esc(title)} · multiversectl</title>
<link rel="canonical" href="${ORIGIN}${to}">
<meta http-equiv="refresh" content="0; url=${to}">
<meta name="robots" content="noindex, follow">
<style>
  :root{color-scheme:light dark;font-family:system-ui,sans-serif}
  body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px}
  p{margin:0;text-align:center;line-height:1.6}
  a{color:inherit}
</style>
</head>
<body>
<p>This page moved to <a href="${to}">${esc(title)}</a>.</p>
</body>
</html>
`;

// Every page in the nav, at the address it used to have. The landing page is
// left out on purpose: it did not move, and /index.html is still the file the
// build writes at the root, so a stub there would collide with the real page.
const moved = pages
  .filter((p) => p.path !== 'index')
  .map((p) => [`/${p.path}.html`, href(p.path), p.title]);

let n = 0;
for (const [from, to, title] of moved) {
  if (from === '/index.html') throw new Error('a stub would overwrite the landing page');
  const out = join(root, 'public', from.replace(/^\//, ''));
  await mkdir(dirname(out), { recursive: true });
  await writeFile(out, stub(to, title), 'utf8');
  n++;
}
console.log(`wrote ${n} redirect stubs into public/`);
