#!/usr/bin/env node
// Check a built or deployed multiversectl site: every page answers, every
// internal link and anchor resolves, and each page carries the metadata a
// search engine needs and nothing it would be penalised for.
//
//   node verify-site.mjs https://multiversectl.dev
//   node verify-site.mjs http://127.0.0.1:4321
//
// It crawls from the root, follows same-origin links only, and exits non-zero
// on the first class of problem it finds, so it can gate a deploy.

const base = (process.argv[2] || 'http://127.0.0.1:4321').replace(/\/$/, '');
const seen = new Map();          // url -> {status, html}
const problems = [];
const PROD = 'https://multiversectl.dev';
// Sitemap and canonical URLs name the production origin; map them onto the
// origin being crawled so they can be compared with the pages reached.
const local = (u) => u.startsWith(PROD) ? base + u.slice(PROD.length) : u;
const say = (...a) => console.log(...a);

const norm = (u) => {
  const x = new URL(u, base);
  x.hash = '';
  // /index.html and / are the same page (the canonical is the root). The
  // trailing slash is kept: every page is published at <path>/, and dropping it
  // would make each check fetch the extensionless form and follow a 301 to get
  // back here.
  x.pathname = x.pathname.replace(/\/index\.html$/, '/');
  return x.href;
};

async function get(url) {
  if (seen.has(url)) return seen.get(url);
  let r;
  try {
    r = await fetch(url, {redirect: 'manual'});
  } catch (e) {
    const rec = {status: 0, html: '', error: String(e)};
    seen.set(url, rec);
    return rec;
  }
  const ct = r.headers.get('content-type') || '';
  const html = ct.includes('text/html') ? await r.text() : '';
  const rec = {status: r.status, html, location: r.headers.get('location'), ct};
  seen.set(url, rec);
  return rec;
}

const attr = (tag, name) => {
  // A value may hold the other kind of quote ("one repository's branch").
  const m = tag.match(new RegExp('(?:^|\\s)' + name + '=(?:"([^"]*)"|\'([^\']*)\')', 'i'));
  return m ? (m[1] ?? m[2]) : null;
};
const tags = (html, re) => html.match(re) || [];

async function crawl() {
  const queue = [base + '/'];
  const pages = [];
  while (queue.length) {
    const url = norm(queue.shift());
    if (seen.has(url)) continue;
    const r = await get(url);
    if (r.status >= 300 && r.status < 400 && r.location) {
      queue.push(norm(r.location));
      continue;
    }
    if (r.status !== 200) {
      problems.push(`${r.status || 'ERR'}  ${url}${r.error ? '  ' + r.error : ''}`);
      continue;
    }
    if (!r.html) continue;
    pages.push(url);
    for (const a of tags(r.html, /<a\s[^>]*href=["'][^"']+["'][^>]*>/gi)) {
      const href = attr(a, 'href');
      if (!href || /^(https?:)?\/\//i.test(href) && !href.startsWith(base)) continue;
      if (/^(mailto:|tel:|#)/i.test(href)) continue;
      const u = new URL(href, url);
      if (u.origin !== new URL(base).origin) continue;
      queue.push(norm(u.href));
    }
  }
  return pages;
}

function checkAnchors(pages) {
  for (const url of pages) {
    const {html} = seen.get(url);
    for (const a of tags(html, /<a\s[^>]*href=["'][^"']*#[^"']+["'][^>]*>/gi)) {
      const href = attr(a, 'href');
      const [path, frag] = href.split('#');
      const target = path ? norm(new URL(path, url).href) : url;
      const rec = seen.get(target);
      if (!rec || !rec.html) continue;
      const has = new RegExp(`id=["']${frag.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}["']`).test(rec.html);
      if (!has) problems.push(`dead anchor  ${url}  ->  ${href}`);
    }
  }
}

/** Every .code block of a page, with its nested markup intact. A shell block
 *  holds a <div> of its own for the window frame, so the closing tag cannot be
 *  found by a non-greedy match — it has to be counted. */
function codeBlocks(html) {
  const out = [];
  const open = /<div class="code (code-\w+)"([^>]*)>/g;
  let m;
  while ((m = open.exec(html))) {
    const from = open.lastIndex;
    let depth = 1, i = from;
    const tag = /<(\/?)div\b[^>]*>/g;
    tag.lastIndex = from;
    let t;
    while (depth > 0 && (t = tag.exec(html))) {
      depth += t[1] ? -1 : 1;
      i = t.index;
    }
    out.push({cls: m[1], attrs: m[2], body: html.slice(from, i)});
  }
  return out;
}

function checkCodeBlocks(pages) {
  // A code block says what it holds, and that decides what Copy gives: a shell
  // gives the commands, a file gives the whole thing, and output alone gives
  // no button at all. The classification is per block and easy to get wrong on
  // a new one, so it is checked rather than trusted.
  let shells = 0, files = 0, outputs = 0;
  for (const url of pages) {
    const {html} = seen.get(url);
    const where = url.replace(base, '') || '/';
    for (const m of codeBlocks(html)) {
      const cls = m.cls, rest = m.attrs, body = m.body;
      const kind = (cls.match(/^code-(shell|file|output)$/) || [])[1];
      const copy = (rest.match(/data-copy="(\w+)"/) || [])[1];
      if (!kind) { problems.push(`a code block says no kind  ${where}`); continue; }
      if (kind === 'shell') {
        shells++;
        if (copy !== 'input') problems.push(`shell block copies "${copy}", want input  ${where}`);
        if (!/<span class="in">/.test(body))
          problems.push(`shell block has no marked input, so Copy would give nothing  ${where}`);
        if (!/class="code-bar"/.test(body))
          problems.push(`shell block has no window frame  ${where}`);
        if (!/aria-hidden="true"/.test(body))
          problems.push(`shell block's window lights are not hidden from a screen reader  ${where}`);
      } else if (kind === 'file') {
        files++;
        if (copy !== 'all') problems.push(`file block copies "${copy}", want all  ${where}`);
        if (/<span class="in">/.test(body))
          problems.push(`file block marks input, which only a shell has  ${where}`);
      } else {
        outputs++;
        if (copy) problems.push(`output-only block offers Copy ("${copy}")  ${where}`);
        if (/class="code-bar"/.test(body))
          problems.push(`output-only block is framed as a terminal window  ${where}`);
      }
    }
  }
  say(`  code blocks: ${shells} shell, ${files} file, ${outputs} output`);
}

function checkSeo(pages) {
  const titles = new Map(), descs = new Map();
  for (const url of pages) {
    const {html} = seen.get(url);
    const where = url.replace(base, '') || '/';

    const title = (html.match(/<title>([^<]*)<\/title>/i) || [])[1];
    if (!title) problems.push(`no <title>  ${where}`);
    else {
      if (title.length > 60) problems.push(`title ${title.length} chars (>60)  ${where}  "${title}"`);
      if (titles.has(title)) problems.push(`duplicate title  ${where}  and  ${titles.get(title)}`);
      titles.set(title, where);
    }

    const desc = tags(html, /<meta\s[^>]*>/gi)
      .map((t) => (attr(t, 'name') === 'description' ? attr(t, 'content') : null))
      .find(Boolean);
    if (!desc) problems.push(`no meta description  ${where}`);
    else {
      if (desc.length < 120) problems.push(`description ${desc.length} chars (<120)  ${where}`);
      if (desc.length > 160) problems.push(`description ${desc.length} chars (>160)  ${where}`);
      if (descs.has(desc)) problems.push(`duplicate description  ${where}  and  ${descs.get(desc)}`);
      descs.set(desc, where);
    }

    const canon = tags(html, /<link\s[^>]*>/gi)
      .map((t) => (attr(t, 'rel') === 'canonical' ? attr(t, 'href') : null))
      .find(Boolean);
    if (!canon) problems.push(`no canonical  ${where}`);
    else {
      // Canonicals are absolute on the production origin, whatever origin is
      // being crawled, and must match this page's path byte for byte.
      const c = new URL(canon, url);
      if (c.origin !== PROD) problems.push(`canonical not on ${PROD}  ${where}  ->  ${canon}`);
      else if (norm(local(canon)) !== url) problems.push(`canonical points elsewhere  ${where}  ->  ${canon}`);
      if (!c.pathname.endsWith('/')) problems.push(`canonical does not end in a slash  ${where}  ->  ${canon}`);
      if (/\.html$/.test(c.pathname)) problems.push(`canonical still names a .html file  ${where}  ->  ${canon}`);
    }

    for (const p of ['og:title', 'og:description', 'og:type', 'og:url', 'og:image', 'og:site_name', 'og:locale']) {
      const has = tags(html, /<meta\s[^>]*>/gi).some((t) => attr(t, 'property') === p);
      if (!has) problems.push(`no ${p}  ${where}`);
    }

    const metas = tags(html, /<meta\s[^>]*>/gi);
    for (const n of ['twitter:title', 'twitter:description', 'twitter:image']) {
      if (!metas.some((t) => attr(t, 'name') === n)) problems.push(`no ${n}  ${where}`);
    }
    const card = metas.find((t) => attr(t, 'name') === 'twitter:card');
    if (!card || attr(card, 'content') !== 'summary_large_image') problems.push(`twitter:card is not summary_large_image  ${where}`);

    // JSON-LD: parses; the home has a SoftwareApplication, docs pages a
    // BreadcrumbList that names the same steps as the visible breadcrumb.
    const ld = [];
    for (const m of html.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)) {
      try { ld.push(JSON.parse(m[1])); } catch { problems.push(`JSON-LD does not parse  ${where}`); }
    }
    const types = ld.map((o) => o['@type']);
    if (where === '/' && !types.some((t) => t === 'SoftwareApplication' || t === 'SoftwareSourceCode')) problems.push(`no SoftwareApplication JSON-LD  ${where}`);
    if (where !== '/' && !types.includes('BreadcrumbList')) problems.push(`no BreadcrumbList JSON-LD  ${where}`);
    const bl = ld.find((o) => o['@type'] === 'BreadcrumbList');
    if (bl) {
      const crumb = (html.match(/<nav class="crumb"[\s\S]*?<\/nav>/) || [''])[0].replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim().replace(' / ', '|');
      const names = bl.itemListElement.map((i) => i.name).join('|');
      if (crumb !== names) problems.push(`BreadcrumbList (${names}) differs from visible breadcrumb (${crumb})  ${where}`);
    }

    const h1s = tags(html, /<h1[\s>]/gi).length;
    if (h1s !== 1) problems.push(`${h1s} <h1> (want 1)  ${where}`);

    if (!/<html[^>]+lang=/i.test(html)) problems.push(`no <html lang>  ${where}`);

    for (const img of tags(html, /<img\s[^>]*>/gi)) {
      const alt = attr(img, 'alt');
      if (alt === null || alt.trim().length < 5) problems.push(`img without a meaningful alt  ${where}`);
      if (!attr(img, 'width') || !attr(img, 'height')) problems.push(`img without width/height  ${where}`);
    }
  }
}

async function checkFiles(pages) {
  for (const f of ['/robots.txt', '/sitemap.xml', '/sitemap-index.xml']) {
    const r = await get(base + f);
    if (r.status === 200) say(`  ${f}  ok`);
    else if (f !== '/sitemap-index.xml' && f !== '/sitemap.xml') problems.push(`missing ${f}`);
  }
  const nf = await get(base + '/404.html');
  if (nf.status !== 200 || !/<h1[\s>]/.test(nf.html) || !/href="\/docs\.html"/.test(nf.html) || !/class="topbar"/.test(nf.html)) problems.push('404.html missing, or lacks the site chrome / a link into the docs');
  const og = await fetch(base + '/og-image.png');
  const buf = Buffer.from(await og.arrayBuffer());
  if (og.status !== 200 || buf.readUInt32BE(16) !== 1200 || buf.readUInt32BE(20) !== 630) problems.push('/og-image.png is not a 1200x630 PNG');
  const rtxt = await (await fetch(base + '/robots.txt')).text();
  if (!/^Sitemap:\s*https:\/\/multiversectl\.dev\/sitemap/mi.test(rtxt)) problems.push('robots.txt does not point at the sitemap');
  if (/^Disallow:\s*\/\s*$/mi.test(rtxt)) problems.push('robots.txt disallows everything');
  const sm = [seen.get(base + '/sitemap.xml'), seen.get(base + '/sitemap-index.xml')]
    .find((r) => r && r.status === 200);
  if (!sm) { problems.push('no sitemap.xml or sitemap-index.xml'); return; }

  const body = sm.html || (await (await fetch(sm === seen.get(base + '/sitemap.xml') ? base + '/sitemap.xml' : base + '/sitemap-index.xml')).text());
  const locs = [...body.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => norm(local(m[1])));
  const subs = locs.filter((l) => /sitemap.*\.xml$/.test(l));
  let urls = locs.filter((l) => !/sitemap.*\.xml$/.test(l));
  for (const s of subs) {
    const t = await (await fetch(local(s))).text();
    urls = urls.concat([...t.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => norm(local(m[1]))));
  }
  const missing = pages.filter((p) => !urls.includes(p));
  const extra = urls.filter((u) => !pages.includes(u));
  if (missing.length) problems.push(`not in the sitemap: ${missing.join(', ')}`);
  for (const u of extra) {
    const r = await get(u);
    if (r.status !== 200) problems.push(`sitemap lists ${r.status}: ${u}`);
  }
  say(`  sitemap lists ${urls.length} urls`);
}

async function checkRedirects(pages) {
  // The documentation moved from /start/install.html to /docs/start/install/.
  // Every one of those old addresses is published, so every one of them has to
  // still land a reader on the page that replaced it. Nothing links to them,
  // so the crawl above cannot have covered them.
  const olds = pages
    .map((u) => new URL(u).pathname)
    .filter((pa) => pa.startsWith('/docs/') || pa === '/docs/')
    .map((pa) => {
      const stem = pa.slice('/docs/'.length).replace(/\/$/, '');
      return [stem ? `/${stem}.html` : '/docs.html', pa];
    });
  let ok = 0;
  for (const [from, to] of olds) {
    const r = await get(base + from);
    if (r.status !== 200) { problems.push(`old address ${from} answers ${r.status || 'ERR'}`); continue; }
    const canon = tags(r.html, /<link\s[^>]*>/gi)
      .map((t) => (attr(t, 'rel') === 'canonical' ? attr(t, 'href') : null)).find(Boolean);
    if (!canon || new URL(canon).pathname !== to)
      problems.push(`old address ${from} has canonical ${canon || 'none'}, want ${to}`);
    const refresh = tags(r.html, /<meta\s[^>]*>/gi)
      .map((t) => (/refresh/i.test(attr(t, 'http-equiv') || '') ? attr(t, 'content') : null)).find(Boolean);
    if (!refresh || !refresh.includes(to))
      problems.push(`old address ${from} has no meta refresh to ${to}`);
    if (!new RegExp(`href="${to}"`).test(r.html))
      problems.push(`old address ${from} has no visible link to ${to}`);
    ok++;
  }
  say(`  ${ok} old addresses still reach their page`);
}

const pages = await crawl();
say(`${base}\n  ${pages.length} pages reached`);
checkAnchors(pages);
checkSeo(pages);
checkCodeBlocks(pages);
await checkFiles(pages);
await checkRedirects(pages);

if (problems.length) {
  say(`\n${problems.length} problem(s):`);
  for (const p of problems) say('  ' + p);
  process.exit(1);
}
say('\nall checks passed');
