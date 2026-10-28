'use strict';

// Everything on the page is built with text nodes, never HTML strings: the data
// comes from files on disk and from git remotes, and none of it is trusted to
// be markup.
const h = (tag, props, ...kids) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid == null || kid === false) continue;
    el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return el;
};
const main = document.getElementById('main');
// One way to write a query string, for every caller that needs one.
const query = (o) => new URLSearchParams(o).toString();
const REFRESH_MS = 10000;

// What the page knows, fed by the websocket. A view reads from it and is told
// when a piece changes; when the socket is down the views fetch for themselves.
const state = { connected: false, readOnly: false, hello: null, update: null, fleet: null, overview: null, ops: [], shown: null, dockOpen: false };

async function api(path) {
  const res = await fetch('/api/' + path, { headers: { Accept: 'application/json' } });
  let body = {};
  try { body = await res.json(); } catch (_) { /* leave empty */ }
  if (!res.ok) throw new Error(body.error || res.statusText);
  return body;
}

// Everything that changes something goes through here. The header is what the
// server asks for on anything that is not a read; a page on another site cannot
// send it, and the session cookie is SameSite=Strict besides.
async function post(path, payload) {
  const res = await fetch('/api/' + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Mvctl': '1' },
    body: JSON.stringify(payload),
  });
  let body = {};
  try { body = await res.json(); } catch (_) { /* leave empty */ }
  if (!res.ok) throw Object.assign(new Error(body.error || res.statusText), { status: res.status, body });
  return body;
}

let toastTimer;
function toast(msg) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 1600);
}

// A value people want in a terminal: one click copies it, and the button says
// so where it was pressed. A message at the foot of the page is not an answer:
// the operations dock and the side panel both sit there, and a copy that
// reports itself behind them reads as a button that does nothing.
function copyable(text, label) {
  const name = label || text;
  let said;
  const b = h('button', {
    type: 'button', class: 'copy mono', title: 'Copy ' + text,
    onclick: async () => {
      const ok = await wrote(text);
      b.classList.remove('copied', 'failed');
      b.classList.add(ok ? 'copied' : 'failed');
      b.replaceChildren(ok ? 'Copied' : 'Press ctrl-C to copy');
      if (!ok) select(b, text);
      clearTimeout(said);
      said = setTimeout(() => { b.classList.remove('copied', 'failed'); b.replaceChildren(name); }, 1400);
    },
  }, name);
  return b;
}

// The clipboard's promise is raced against a second of patience. A browser
// that will not write does not always refuse: asked while the page is not the
// focused one, it answers neither way, and a button that waits on it forever
// is a button that does nothing when pressed.
function wrote(text) {
  const clip = navigator.clipboard;
  if (!clip) return Promise.resolve(false);
  return Promise.race([
    clip.writeText(text).then(() => true, () => false),
    new Promise((say) => setTimeout(() => say(false), 1000)),
  ]);
}

// When the clipboard is refused — it is, on a page served over plain http to
// something other than localhost — the next best thing is to select the text
// so the keyboard can take it.
function select(near, text) {
  const hold = h('span', { class: 'picked mono' }, text);
  near.after(hold);
  const range = document.createRange();
  range.selectNodeContents(hold);
  const sel = getSelection();
  sel.removeAllRanges();
  sel.addRange(range);
  setTimeout(() => hold.remove(), 8000);
}

// A repository as a short name: owner/repo from a URL.
const shortRepoName = (u) => u.replace(/^[a-z+]+:\/\//, '').replace(/^[^@/]*@/, '').replace(/:/, '/').replace(/\.git$/, '');
const badge = (kind, text, title) => h('span', { class: 'badge ' + kind, title }, text);
const empty = (...kids) => h('div', { class: 'empty' }, kids);
// The shape of what is coming, while it is read. A page that holds its layout
// is a page that does not jump when the numbers land, and “Loading…” holds
// nothing: the word is for the screen reader, which cannot see the shape.
// Counting out loud, in the one place that knows the rule.
const plural = (n, one, many) => n + ' ' + (n === 1 ? one : many);
const skeleton = (cls) => h('span', { class: 'skel ' + (cls || ''), 'aria-hidden': 'true' });
const cmd = (c) => h('code', null, c);
const errorBox = (e) => h('div', { class: 'error', role: 'alert' }, String(e.message || e));

// Where something lives is drawn the same way everywhere: the platform's mark and
// its name, then the path on it (github:owner/repo, gitlab:group/repo,
// file:~/path), so the kind of place reads before the name does. The marks are
// built with the DOM, like everything else here, never from markup strings.
const SVG_NS = 'http://www.w3.org/2000/svg';
const ICONS = {
  github: { d: 'M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z' },
  gitlab: { d: 'M8 14.9L.9 9.6l1.1-3.3L3.7 1.7c.1-.3.6-.3.7 0L6 6.3h4l1.6-4.6c.1-.3.6-.3.7 0L14 6.3l1.1 3.3z' },
  git: { d: 'M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25zm-6 0a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0zm8.25-.75a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5zM4.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5z' },
  file: { d: 'M1.5 3A1.5 1.5 0 0 1 3 1.5h3.1c.4 0 .78.16 1.06.44L8.2 3H13A1.5 1.5 0 0 1 14.5 4.5v7A1.5 1.5 0 0 1 13 13H3a1.5 1.5 0 0 1-1.5-1.5z' },
  root: { d: 'M8 1.2l2.1 4.4 4.8.7-3.5 3.4.8 4.8L8 12.2l-4.2 2.3.8-4.8L1.1 6.3l4.8-.7z' },
  catalog: { d: 'M2 3.5h12v1.6H2zM2 7.2h12v1.6H2zM2 10.9h8v1.6H2z' },
  // GitHub's marks for the four states of a pull request.
  prOpen: { d: 'M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z' },
  prDraft: { d: 'M3.25 1A2.25 2.25 0 0 1 4 5.372v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.251 2.251 0 0 1 3.25 1Zm9.5 14a2.25 2.25 0 1 1 0-4.5 2.25 2.25 0 0 1 0 4.5ZM2.5 3.25a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0ZM3.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm9.5 0a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM14 7.5a1.25 1.25 0 1 1-2.5 0 1.25 1.25 0 0 1 2.5 0Zm0-4.25a1.25 1.25 0 1 1-2.5 0 1.25 1.25 0 0 1 2.5 0Z' },
  prMerged: { d: 'M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0 0 .005V3.25Z' },
  prClosed: { d: 'M3.25 1A2.25 2.25 0 0 1 4 5.372v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.251 2.251 0 0 1 3.25 1Zm9.5 5.5a.75.75 0 0 1 .75.75v3.378a2.251 2.251 0 1 1-1.5 0V7.25a.75.75 0 0 1 .75-.75Zm-2.03-5.273a.75.75 0 0 1 1.06 0l.97.97.97-.97a.748.748 0 0 1 1.265.332.75.75 0 0 1-.205.729l-.97.97.97.97a.751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018l-.97-.97-.97.97a.749.749 0 0 1-1.275-.326.749.749 0 0 1 .215-.734l.97-.97-.97-.97a.75.75 0 0 1 0-1.06ZM2.5 3.25a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0ZM3.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm9.5 0a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z' },
  multiverse: { d: 'M8 1.5l5.5 3v7L8 14.5l-5.5-3v-7zM2.5 4.5L8 7.5l5.5-3M8 7.5v7', stroke: true },
};

function svg(name, cls) {
  const icon = ICONS[name] || ICONS.git;
  const el = document.createElementNS(SVG_NS, 'svg');
  el.setAttribute('viewBox', '0 0 16 16');
  el.setAttribute('width', '14');
  el.setAttribute('height', '14');
  el.setAttribute('aria-hidden', 'true');
  el.setAttribute('class', cls || '');
  const path = document.createElementNS(SVG_NS, 'path');
  path.setAttribute('d', icon.d);
  if (icon.stroke) {
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke', 'currentColor');
    path.setAttribute('stroke-width', '1.3');
    path.setAttribute('stroke-linejoin', 'round');
  } else {
    path.setAttribute('fill', 'currentColor');
  }
  el.append(path);
  return el;
}

// place draws a location the server worked out: its platform mark, the platform
// named, and the rest. within is the file inside the repository, when that is
// what tells this node from another in the same one.
function place(loc, within) {
  if (!loc) return null;
  const i = loc.short.indexOf(':');
  return h('span', { class: 'place ' + loc.platform, title: loc.url || loc.short },
    svg(loc.platform, 'pi'), h('span', { class: 'plat' }, loc.short.slice(0, i)), h('span', { class: 'sep' }, ':'),
    h('span', { class: 'path' }, loc.short.slice(i + 1)), within ? h('span', { class: 'within' }, within) : null);
}

function ago(iso) {
  if (!iso) return '';
  const s = (Date.now() - Date.parse(iso)) / 1000;
  if (s < 90) return 'just now';
  const units = [[86400 * 30, 'mo', 86400 * 30], [86400, 'd', 86400], [3600, 'h', 3600], [60, 'min', 60]];
  for (const [limit, name, div] of units) if (s >= limit) return Math.floor(s / div) + ' ' + name + ' ago';
  return 'just now';
}

// ── views ──────────────────────────────────────────────────────────────
// A view is { title, mount(host) -> { refresh() } }: mount builds its static
// parts once, and refresh refills only what changes, so a filter box keeps
// the focus and a tree keeps its open branches while the data moves.

const views = {};

views.overview = {
  title: 'Overview',
  mount(host) {
    const body = h('div');
    host.append(h('h1', null, 'Overview'),
      h('p', { class: 'lede' }, 'This machine: the root catalog, what is installed, and where it has drifted.'),
      h('div', { class: 'bar' },
        h('button', { type: 'button', class: 'btn', title: 'git fetch every catalog and manifest, then compare',
          onclick: async (e) => { e.target.disabled = true; try { await refresh(true); toast('Compared with the remotes'); } finally { e.target.disabled = false; } } },
        'Fetch remotes and compare')), body);
    let last = null;
    const draw = (d) => { last = d; body.replaceChildren(...overview(d.status, d.rootLoc, d.sources || {})); };
    return {
      async refresh(fetch) {
        try {
          const d = await api('overview' + (fetch ? '?fetch=1' : ''));
          if (d.readOnly) state.readOnly = true;
          draw(d);
        } catch (e) { body.replaceChildren(errorBox(e)); }
      },
      onOverview(d) { draw(d); },
      onOps() { if (last) draw(last); },
      onMode() { if (last) draw(last); },
    };
  },
};

function overview(st, rootLoc, sources) {
  const out = [];
  if (st.next) out.push(h('div', { class: 'next' }, h('b', null, 'Next'), copyable(st.next)));

  const root = st.catalog || {};
  const rootCard = h('div', { class: 'card' }, h('h3', null, 'Catalog'));
  if (!root.source) {
    rootCard.append(h('p', { class: 'muted' }, 'None. Multiverses can still be installed from a repository.'),
      h('p', null, cmd('multiversectl init')));
  } else {
    rootCard.append(h('div', null, place(rootLoc) || h('span', { class: 'mono wrap' }, root.source), ' ',
      root.unavailable ? null : badge('ok', 'Catalog')));
    if (root.unavailable) rootCard.append(h('p', null, badge('bad', 'unavailable'), ' ', root.unavailable));
    if (root.noRemote) rootCard.append(h('p', null, badge('warn', 'local only'), ' Lost with this machine, and nobody else can use it.'));
    rootCard.append(h('p', null, h('a', { href: '#/catalog' }, 'Browse the tree →')));
  }
  const ws = st.multiverses || [];
  const stale = ws.filter((w) => w.stale).length;
  out.push(h('div', { class: 'cards' },
    rootCard,
    h('div', { class: 'card' }, h('h3', null, 'Installed'), h('div', { class: 'big' }, ws.length),
      h('div', { class: 'muted' }, ws.length === 1 ? 'multiverse' : 'multiverses')),
    h('div', { class: 'card' }, h('h3', null, 'Drift'), h('div', { class: 'big' }, stale),
      h('div', { class: 'muted' }, stale ? 'need an update' : 'all up to date'))));

  out.push(h('h2', null, 'Multiverses'));
  if (!ws.length) {
    out.push(empty('Nothing installed yet.', h('br'), cmd('multiversectl start <owner/repo>')));
    return out;
  }
  out.push(h('div', { class: 'scroll' }, h('table', null,
    h('thead', null, h('tr', null, ['Multiverse', 'Catalog path', 'Source', 'State', 'Root', ''].map((t) => h('th', null, t)))),
    h('tbody', null, ws.map((w) => h('tr', null,
      h('td', null,
        h('a', { href: '#/multiverse/' + encodeURIComponent(w.name) }, w.name)),
      h('td', { class: 'mono' }, w.from || '—'),
      h('td', null, place(sources[w.name]) || h('span', { class: 'muted' }, '—')),
      h('td', null, w.notes && w.notes.length
        ? w.notes.map((n) => [badge(w.stale ? 'warn' : 'mute', n), ' '])
        : badge('ok', 'up to date')),
      h('td', null, copyable(w.root)),
      h('td', { class: 'row-actions' }, wsActions(w, { update: true }))))))));
  return out;
}

views.catalog = {
  title: 'Catalog',
  mount(host) {
    const open = new Set(['']);
    let filter = '', data = null, first = true, loading = false;
    // How each repository is doing while the catalogs are read, by repository:
    // they are read at the same time, so the page shows them as they answer.
    const steps = new Map();
    const body = h('div');
    const live = h('div', { class: 'card live-fetch', hidden: true });
    const search = h('input', {
      type: 'search', name: 'filter', placeholder: 'Filter by name, path or repository   ( / )', 'aria-label': 'Filter the catalog',
      oninput: (e) => { filter = e.target.value.trim().toLowerCase(); draw(); },
    });
    const all = (state) => () => {
      const walk = (n) => { if (n.kind === 'catalog') { state ? open.add(n.path) : open.delete(n.path); (n.children || []).forEach(walk); } };
      if (data) { walk(data.root); open.add(''); draw(); }
    };
    host.append(h('h1', null, 'Catalog'),
      h('p', { class: 'lede' }, 'Your index of multiverses, as a tree. Nodes are addressed by path, like acme/backend/billing.'),
      h('div', { class: 'bar' }, search,
        h('button', { type: 'button', class: 'btn', onclick: all(true) }, 'Expand all'),
        h('button', { type: 'button', class: 'btn', onclick: all(false) }, 'Collapse all'),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'btn', onclick: (e) => load(true, e.target) }, 'Fetch remotes')),
      live, body);

    const STEP = { fetching: ['info', 'reading…'], done: ['ok', '✓ up to date'], stale: ['warn', '⚠ not refreshed'], unavailable: ['bad', '✗ unavailable'] };
    const stepBadge = (st) => {
      const [kind, text] = STEP[st.phase] || ['mute', st.phase];
      const secs = st.ms ? ' · ' + (st.ms / 1000).toFixed(1) + 's' : '';
      return h('span', { class: 'step' }, st.phase === 'fetching' ? h('span', { class: 'spin', role: 'status', 'aria-label': 'reading' }) : null,
        badge(kind, text + secs, st.detail || null), st.detail && st.phase !== 'fetching' ? h('span', { class: 'muted' }, st.detail) : null);
    };
    function drawLive() {
      live.hidden = !loading || steps.size === 0;
      if (live.hidden) return;
      const rows = [...steps.entries()].map(([repo, st]) => h('li', null, h('span', { class: 'mono' }, shortRepoName(repo)), stepBadge(st)));
      const done = [...steps.values()].filter((st) => st.phase !== 'fetching').length;
      live.replaceChildren(h('h3', null, 'Reading catalogs  ', h('span', { class: 'muted' }, done + ' of ' + steps.size)), h('ul', { class: 'steps' }, rows));
    }
    let scheduled = false;
    function onProgress(e) {
      if (!loading) return;
      steps.set(e.repo + (e.ref ? '@' + e.ref : ''), { phase: e.phase, detail: e.detail, ms: e.ms });
      if (scheduled) return;
      scheduled = true;
      requestAnimationFrame(() => { scheduled = false; drawLive(); draw(); });
    }

    const matches = (n) => !filter || [n.name, n.path, n.repo || '', n.loc ? n.loc.short : '', n.within || ''].some((s) => s.toLowerCase().includes(filter));
    const keeps = (n) => matches(n) || (n.children || []).some(keeps);

    function item(n) {
      if (!keeps(n)) return null;
      // What kind of node it is, said in words and drawn, and where it lives:
      // two catalogs can share a repository and differ only by their file.
      const kind = n.path === '' ? 'root' : n.kind;
      const label = [
        svg(kind, 'ki ' + kind),
        h('span', { class: 'name' }, n.name),
        h('span', { class: 'kind ' + kind }, kind),
        place(n.loc, n.within),
        n.unavailable ? badge('bad', '✗ ' + n.unavailable) : null,
        n.stale ? badge('warn', '⚠ not refreshed', n.stale + ' — shown as last fetched') : null,
        loading && n.repo && n.kind === 'catalog' && steps.has(n.repo + (n.ref ? '@' + n.ref : '')) ? stepBadge(steps.get(n.repo + (n.ref ? '@' + n.ref : ''))) : null,
        n.kind === 'multiverse' && !n.unavailable ? (n.installedAs ? badge('ok', '✓ installed as ' + n.installedAs) : badge('mute', '○ not installed')) : null,
      ];
      if (n.kind === 'multiverse') {
        return h('li', { class: 'leaf' }, h('div', { class: 'node ws' }, label,
          n.installedAs ? null : copyable('multiversectl start ' + n.path, 'install')));
      }
      const kids = (n.children || []).map(item).filter(Boolean);
      const d = h('details', { ontoggle: (e) => { e.target.open ? open.add(n.path) : open.delete(n.path); } },
        h('summary', null, h('div', { class: 'node cat' }, label)),
        kids.length ? h('ul', null, kids) : h('p', { class: 'muted tree-empty' }, n.unavailable ? '' : 'empty'));
      if (open.has(n.path) || filter) d.open = true;
      return h('li', null, d);
    }
    function draw() {
      if (!data) return;
      const root = item(data.root);
      body.replaceChildren(root ? h('ul', { class: 'tree', role: 'tree' }, root)
        : empty('Nothing matches ', cmd(filter)));
      if (data.root.children == null && !filter) {
        body.append(empty('The catalog is empty.', h('br'), cmd('multiversectl catalog add <owner/repo>')));
      }
    }
    async function load(fetch, btn) {
      if (btn) btn.disabled = true;
      loading = true; steps.clear();
      try {
        data = await api('catalog' + (fetch ? '?fetch=1' : ''));
        // Open the tree the first time, so the multiverses are in view; a large
        // catalog stays closed, and the filter opens what it finds.
        if (first) { first = false; const count = (n) => 1 + (n.children || []).reduce((a, c) => a + count(c), 0); if (count(data.root) <= 150) all(true)(); }
        draw(); if (fetch) toast('Fetched');
      }
      catch (e) { body.replaceChildren(errorBox(e)); if (/no root catalog/.test(e.message)) body.append(empty(cmd('multiversectl init'))); }
      finally { loading = false; if (btn) btn.disabled = false; drawLive(); draw(); }
    }
    return { refresh: () => load(false), focus: () => search.focus(), onCatalogProgress: onProgress };
  },
};

const STATE_DOT = { running: 'ok', stopped: 'warn', 'not started': '', optional: '', off: '' };

function universeCard(w, u) {
  const total = u.apps.reduce((n, a) => n + a.expected, 0);
  const busy = isBusy(w.name);
  const act = (action, label, kind) => h('button', { type: 'button', class: 'btn small ' + (kind || ''), disabled: busy,
    title: busy ? 'Another operation is running in ' + w.name : null,
    onclick: () => run({ action, multiverse: w.name, universe: u.name }) }, label);
  const actions = state.readOnly ? null : h('div', { class: 'actions' },
    u.running < total || !u.running ? act('up', 'Start', 'primary') : null,
    u.running ? act('down', 'Stop') : null,
    u.behind ? act('sync', 'Update code') : null,
    h('button', { type: 'button', class: 'btn small danger', disabled: busy, onclick: () => removeDialog(w, u) }, 'Remove…'));
  const apps = u.apps.map((a) => h('div', { class: 'app' },
    h('div', { class: 'app-head' }, h('b', null, a.key),
      // The branch is the same for every app of a universe in all but the
      // rarest case, and it is in the header; the repository is only worth
      // naming when it is not the app's own name.
      a.repo && a.repo !== a.key ? h('span', { class: 'muted small' }, a.repo) : null,
      a.expected ? badge(a.running === a.expected ? 'ok' : a.running ? 'warn' : 'mute', a.running + '/' + a.expected + ' up') : null),
    a.services && a.services.length ? h('div', { class: 'svcs' }, a.services.map((s) =>
      h('span', { class: 'svc', title: [s.state, s.detail, s.published && 'publishes ' + s.published].filter(Boolean).join(' · ') },
        h('span', { class: 'dot ' + (STATE_DOT[s.state] || '') }), s.name, s.published ? h('span', { class: 'badge warn' }, s.published) : null))) : null,
    a.urls && a.urls.length ? h('div', { class: 'urls' }, a.urls.map((x) =>
      h('a', { href: x.url, target: '_blank', rel: 'noopener noreferrer', title: x.service }, x.url))) : null));
  const panel = h('div');
  const saved = doctorResults.get(w.name + '/' + u.name);
  if (saved) panel.append(saved);
  return h('article', { class: 'card uni' },
    h('header', null, h('span', { class: 'dot ' + (u.running ? 'ok' : '') }), h('h3', null, u.name),
      branchChip(u),
      badge(u.running ? 'ok' : 'mute', u.running ? u.running + (total ? '/' + total : '') + ' running' : 'stopped'),
      u.behind ? badge('warn', u.behind + ' behind', 'Its code lacks commits of the branches it follows (as of the last fetch). “Update code” brings them in; restart to run them.') : null,
      h('span', { class: 'grow' }),
      h('a', { class: 'btn small', href: universeHref(w.name, u.name), title: 'This universe on a page of its own: its code, files and diffs' }, 'Open'),
      h('button', { type: 'button', class: 'btn small', title: 'Containers, stats and logs', onclick: () => sidePanel.open(w.name, u.name) }, 'Details'),
      h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, u.name, e.target) }, 'Doctor')),
    actions,
    h('div', { class: 'meta' }, [u.created && 'created ' + ago(u.created), u.lastStarted && 'started ' + ago(u.lastStarted), u.domain].filter(Boolean).join(' · ')),
    // Only a web address becomes a link: the field is free text that anyone with a shell wrote.
    u.pr ? h('p', { class: 'note' }, /^https?:\/\//.test(u.pr) ? h('a', { href: u.pr, target: '_blank', rel: 'noopener noreferrer' }, u.pr) : u.pr) : null,
    u.note ? h('p', { class: 'note' }, u.note) : null,
    apps, codeSection(w, u), panel);
}

// The branch is the universe's own name for its work, and in all but the
// rarest case every repository is on the same one: say it once in the header
// rather than once per app and again per path.
function branchChip(u) {
  const names = [...new Set(Object.values(u.branches || {}).filter(Boolean))];
  if (!names.length) return null;
  if (names.length === 1) return h('span', { class: 'chip branch', title: 'Every repository is on ' + names[0] }, names[0]);
  return h('span', { class: 'chip branch', title: names.join(', ') }, names.length + ' branches');
}

// ── what the branches have become ──────────────────────────────────────
//
// How far each repository of a universe has moved from the branch it follows,
// how much work that is, and the pull or merge request open for it. It is a
// request of its own, made once a universe is in view: every row costs a few
// git calls and a question to GitHub or GitLab, and the fleet listing this
// page draws from is pushed at it whenever anything on the machine changes.
const codeState = new Map();
const CODE_STALE = 300000;
// A universe in view is read again on its own, because a review is merged and
// a branch is pushed from somewhere else; the remotes are asked less often than
// that, since it is the part that costs.
const CODE_EVERY = 60000;
const CODE_FETCH_EVERY = 300000;
const codeKey = (multiverse, universe) => JSON.stringify([multiverse, universe]);

async function loadCode(key, again, fetch) {
  const had = codeState.get(key) || {};
  if (had.loading || (!again && had.at && Date.now() - had.at < CODE_STALE)) return;
  codeState.set(key, { ...had, loading: true });
  fillCode(key);
  const [multiverse, universe] = JSON.parse(key);
  const asked = fetch || (had.fetchAt && Date.now() - had.fetchAt > CODE_FETCH_EVERY);
  try {
    const d = await api('universe/code?' + query(asked ? { multiverse, universe, fetch: 1 } : { multiverse, universe }));
    codeState.set(key, { rows: d.rows || [], fetched: d.fetched, at: Date.now(), fetchAt: asked ? Date.now() : (had.fetchAt || Date.now()) });
  } catch (e) {
    codeState.set(key, { ...had, loading: false, error: e.message, at: Date.now() });
  }
  fillCode(key);
  // What is open under a row is as old as the row was.
  if (diffPage && diffPage.key === key) diffPage.reload();
}

// A card is built again whenever anything in the fleet changes, so what was
// read is filled into whatever element is on the page now, found by its key
// rather than kept in a closure that outlives the card it was made for.
function fillCode(key) {
  for (const sec of document.querySelectorAll('.code')) if (sec.dataset.key === key) drawCode(sec);
  if (diffPage && diffPage.key === key) diffPage.paint();
}

// Nothing is read for a universe nobody has scrolled to.
const codeWatch = self.IntersectionObserver ? new IntersectionObserver((seen, obs) => {
  for (const e of seen) {
    if (!e.isIntersecting) continue;
    obs.unobserve(e.target);
    loadCode(e.target.dataset.key);
  }
}, { rootMargin: '200px' }) : null;

// The ones on the page, and in a tab somebody is looking at, are read again.
setInterval(() => {
  if (document.hidden) return;
  const seen = new Set();
  if (diffPage) { seen.add(diffPage.key); loadCode(diffPage.key, true); }
  for (const sec of document.querySelectorAll('.code')) {
    const key = sec.dataset.key;
    if (seen.has(key) || !(codeState.get(key) || {}).rows) continue;
    seen.add(key);
    loadCode(key, true);
  }
}, CODE_EVERY);

function codeSection(w, u) {
  const key = codeKey(w.name, u.name);
  const sec = h('section', { class: 'code', 'data-key': key },
    h('div', { class: 'code-head' }, h('b', null, 'Code'),
      h('span', { class: 'muted small' }, plural(u.checkouts.length, 'repository', 'repositories')),
      h('span', { class: 'grow' }),
      h('span', { class: 'muted small code-age' }),
      copyable(u.dir, 'folder'),
      h('button', { type: 'button', class: 'btn small ghost', title: 'Ask the remotes, then read the branches and their requests again', onclick: () => loadCode(key, true, true) }, 'Reread')),
    h('div', { class: 'code-body' }));
  drawCode(sec);
  if (codeWatch) codeWatch.observe(sec); else loadCode(key);
  return sec;
}

function drawCode(sec) {
  const key = sec.dataset.key;
  const st = codeState.get(key) || {};
  const body = sec.querySelector('.code-body');
  const age = sec.querySelector('.code-age');
  if (age) {
    age.textContent = st.loading ? 'reading…' : st.fetched ? 'remotes read ' + ago(st.fetched) : '';
    age.title = st.fetched ? 'The last time the clones asked their remotes, which is how old what is measured against them can be. Reread asks again.' : '';
  }
  if (!st.rows) {
    body.replaceChildren(st.error ? errorBox({ message: st.error })
      : h('div', { class: 'crepo loading' }, skeleton('line w3'), skeleton('line w2'), skeleton('line w4'), skeleton('line w3')));
    return;
  }
  // The shared branch is named once, in the card's header; a row says its own
  // only when it is not that one, which is the row worth noticing.
  const common = [...new Set(st.rows.map((r) => r.branch).filter(Boolean))];
  body.replaceChildren(...[
    st.error ? h('p', { class: 'muted small' }, badge('warn', 'the last read failed'), ' ', st.error) : null,
    ...st.rows.map((r) => codeRow(key, r, common.length === 1, sec.dataset.mode === 'page')),
  ].filter(Boolean));
}

function codeRow(key, r, shared, page) {
  const review = reviewOf(r);
  const top = [shared || !r.branch ? null : h('span', { class: 'chip branch', title: 'The branch checked out in this repository' }, r.branch), review].filter(Boolean);
  return h('div', { class: 'crepo-block' },
    h('div', { class: 'crepo' },
      h('b', { class: 'crepo-name' }, r.repo),
      h('div', { class: 'crepo-main' },
        top.length ? h('div', { class: 'crepo-top' }, ...top) : null,
        h('div', { class: 'crepo-facts' }, ...factsOf(key, r, page)))));
}

// What became of a repository, each fact in words and on its own: a terse
// “+31 −4 3 files vs main · 2 not pushed” makes somebody work out which number
// belongs to which question. What can be opened has a caret.
//
//   changed against base     what the branch amounts to, which is what a review shows
//   not pushed               commits that are only here
//   not committed            edits that are only in the checkout
//   behind                   commits somebody else added to what it follows
function factsOf(key, r, page) {
  const out = [];
  const [multiverse, universe] = JSON.parse(key);
  const lines = (n) => [h('b', { class: 'add' }, '+' + n.added), h('b', { class: 'del' }, '−' + n.removed)];
  // On a card a change is a link to the universe's page; on that page it picks
  // the change to read there.
  const opener = (scope, text, n, title) => {
    if (page === 'tabs') return null; // the tabs next to them are what opens a change
    const on = page && diffPage && diffPage.repo === r.repo && diffPage.scope === scope;
    const kids = [h('span', { class: 'caret' }, on ? '▾' : '▸'), text, ...lines(n)];
    return page
      ? h('button', { type: 'button', class: 'fact opener' + (on ? ' on' : ''), 'aria-pressed': on ? 'true' : 'false', title: title + ' Click to read its files and diff below.',
        onclick: () => diffPage && diffPage.select(r.repo, scope) }, ...kids)
      : h('a', { class: 'fact opener', href: universeHref(multiverse, universe, r.repo, scope), title: title + ' Click to read its files and diff.' }, ...kids);
  };
  const note = (kind, text, title) => h('span', { class: 'fact ' + kind, title }, text);

  if (r.change) {
    out.push(opener('branch', plural(r.change.files, 'file', 'files') + ' changed against ' + r.base, r.change,
      'What this branch adds to ' + r.base + ': what a pull request would show.'));
  } else if (r.baseUnknown) {
    // Not the same as nothing changed: the base is not in the clone to compare with.
    out.push(note('warn', 'Could not compare with ' + r.base, 'origin/' + r.base + ' is not in this clone, perhaps not fetched yet. Reread asks the remotes.'));
  } else if (r.base) {
    out.push(note('quiet', 'No changes against ' + r.base, 'This branch has nothing that ' + r.base + ' lacks.'));
  } else {
    out.push(note('quiet', 'Nothing to compare it with', 'This repository has no usable origin to measure against.'));
  }
  if (r.ahead && r.follows) {
    out.push(r.follows === r.base
      ? note('mute', plural(r.ahead, 'commit', 'commits') + ', branch not pushed yet', 'These commits are only on this machine: ' + r.follows + ' on origin does not have them.')
      : note('mute', plural(r.ahead, 'commit', 'commits') + ' not pushed', 'These commits are only on this machine, not on origin/' + r.follows + (r.work ? ': ' + plural(r.work.files, 'file', 'files') + ', +' + r.work.added + ' \u2212' + r.work.removed + '.' : '.')));
  }
  if (r.uncommitted) {
    out.push(opener('uncommitted', plural(r.uncommitted.files, 'file', 'files') + ' edited, not committed', r.uncommitted,
      'Edits in the checkout that no commit holds yet, new files included.'));
  }
  if (r.behind) {
    out.push(note('warn', plural(r.behind, 'commit', 'commits') + ' behind ' + r.follows, 'It lacks ' + plural(r.behind, 'commit', 'commits') + ' of ' + r.follows + ', as of the last fetch. “Update code” brings them in.'));
  }
  return out.filter(Boolean);
}

// ── a universe's own page ──────────────────────────────────────────────
//
// The card says how each repository stands; what changed in it is read here,
// where there is room: #/universe/<multiverse>/<universe>[/<repo>/<scope>].
// Which change is open is in the address, so it can be sent to somebody.
const universeHref = (...parts) => '#/universe/' + parts.filter((x) => x != null).map(encodeURIComponent).join('/');
const STATUS_MARK = { added: 'A', modified: 'M', deleted: 'D', renamed: 'R', untracked: 'U' };
const SCOPES = { branch: 'Branch', uncommitted: 'Not committed' };

// The page that is on screen, so the facts of a repository can open its diff
// on it instead of going to it.
let diffPage = null;

views.universe = {
  title: 'Universe',
  mount(host, multiverse, rest) {
    const [name, repo, scope] = rest || [];
    if (!name) { location.hash = '#/fleet'; return {}; }
    const key = codeKey(multiverse, name);
    const head = h('div');
    const browser = h('section', { class: 'cdiff-page' });
    // What the card showed the branch's change against, so the files are of the
    // same change: asking the hosting service again could say otherwise.
    const baseShown = () => (dv.scope === 'branch' ? (((codeState.get(key) || {}).rows || []).find((r) => r.repo === dv.repo) || {}).base || '' : '');
    // These are made once and filled again, never replaced: the filter box is in
    // one of them, and a box that is taken out of the page while somebody types in
    // it loses what they were doing.
    const barBox = h('div'), topBox = h('div'), listBox = h('div', { class: 'cdiff-files' });
    const filterInput = h('input', { type: 'search', name: 'filter-files', placeholder: 'Filter files', 'aria-label': 'Filter the changed files',
      oninput: (e) => { dv.filter = e.target.value.trim().toLowerCase(); paintTree(); } });
    const treeList = h('div', { class: 'ftree-list' });
    const treeBox = h('nav', { class: 'ftree', 'aria-label': 'Changed files' }, filterInput, treeList);
    const layout = h('div', { class: 'cdiff-layout' }, treeBox, listBox);
    browser.append(barBox, topBox, layout);
    host.append(h('p', { class: 'crumbs' }, h('a', { href: '#/fleet' }, '← Fleet'), ' / ', multiverse), head, browser);
    let picked = false;

    // What is open: the change (a repository and a scope), the files whose
    // diff is showing, and the ones pinned to stay showing. Opening a file
    // closes the others unless they are pinned, because a page of diffs nobody
    // can find the top of is worse than one click to reopen.
    const dv = diffPage = { key, paint: () => draw(), repo: null, scope: 'branch', files: null, base: '', open: new Set(), pinned: new Set(), patches: new Map(), error: null,
      filter: '', collapsed: new Set(), active: null,
      async select(r, sc) {
        const same = this.repo === r && this.scope === sc;
        Object.assign(this, { repo: r, scope: sc });
        if (!same) {
          Object.assign(this, { files: null, base: '', sig: '', open: new Set(), pinned: new Set(), patches: new Map(), error: null, filter: '', collapsed: new Set(), active: null });
          filterInput.value = '';
        }
        history.replaceState(null, '', universeHref(multiverse, name, r, sc));
        draw();
        await this.reload();
      },
      async reload() {
        if (!this.repo) return;
        // One read at a time; one asked for meanwhile (another repository, say) waits.
        if (this.busy) { this.again = true; return; }
        this.busy = true;
        const mine = [this.repo, this.scope];
        try {
          const d = await api('universe/diff?' + query({ multiverse, universe: name, repo: this.repo, scope: this.scope, base: baseShown() }));
          if (this.repo !== mine[0] || this.scope !== mine[1]) return;
          // Read again on a timer, and most of the time nothing moved: then nothing
          // is redrawn, so what somebody is reading keeps its place, its selection
          // and its focus.
          const sig = JSON.stringify(d);
          if (this.files && sig === this.sig) return;
          this.sig = sig;
          this.files = d.files || []; this.base = d.base; this.error = null;
          const paths = new Set(this.files.map((f) => f.path));
          for (const p of [...this.open]) if (!paths.has(p)) { this.open.delete(p); this.pinned.delete(p); }
          this.patches = new Map();
          // The first change of the list is what somebody came to see.
          if (!this.open.size && this.files.length) { this.open.add(this.files[0].path); this.active = this.files[0].path; }
          if (this.active && !paths.has(this.active)) this.active = null;
          // What was open is as old as the list: read again, so it cannot disagree.
          await Promise.all([...this.open].map((p) => this.patch(p)));
        } catch (e) { this.error = e.message; } finally { this.busy = false; }
        draw();
        if (this.again) { this.again = false; await this.reload(); }
      },
      async patch(path) {
        try {
          this.patches.set(path, await api('universe/diff?' + query({ multiverse, universe: name, repo: this.repo, scope: this.scope, base: baseShown(), file: path })));
        } catch (e) { this.patches.set(path, { error: e.message }); }
      },
      // Opening a file closes the others unless they are pinned. Closing is
      // only for the file's own row: the tree never closes anything.
      async show(path, scroll) {
        this.active = path;
        if (!this.open.has(path)) {
          for (const p of [...this.open]) if (!this.pinned.has(p)) this.open.delete(p);
          this.open.add(path);
        }
        draw();
        if (scroll) requestAnimationFrame(() => [...listBox.querySelectorAll('li[data-path]')].find((li) => li.dataset.path === path)?.scrollIntoView({ block: 'start', behavior: 'smooth' }));
        if (!this.patches.has(path)) { await this.patch(path); draw(); }
      },
      async toggle(path) {
        if (this.open.has(path)) { this.open.delete(path); this.pinned.delete(path); draw(); return; }
        await this.show(path, false);
      },
      pin(path) { if (this.pinned.has(path)) this.pinned.delete(path); else this.pinned.add(path); draw(); },
    };

    // The change a repository opens on: what the branch amounts to, or, with
    // nothing committed, what is edited.
    const firstScope = (r) => (r.change || !r.uncommitted ? 'branch' : 'uncommitted');

    // Roving focus, as a tab list does: the arrows move between the tabs and
    // Tab leaves the list.
    const arrows = (e) => {
      const keys = { ArrowRight: 1, ArrowLeft: -1, Home: 'first', End: 'last' };
      if (!(e.key in keys)) return;
      const tabs = [...e.currentTarget.querySelectorAll('[role=tab]')];
      const at = tabs.indexOf(document.activeElement);
      const to = keys[e.key] === 'first' ? 0 : keys[e.key] === 'last' ? tabs.length - 1 : (at + keys[e.key] + tabs.length) % tabs.length;
      e.preventDefault();
      tabs[to].focus();
      tabs[to].click();
    };

    function draw() {
      const st = codeState.get(key) || {};
      const rows = st.rows || [];
      if (!dv.repo && rows.length && !picked) {
        picked = true;
        const r0 = rows.find((r) => r.change || r.uncommitted) || rows[0];
        dv.select(r0.repo, firstScope(r0));
        return;
      }
      const row = rows.find((r) => r.repo === dv.repo) || {};
      const repoTabs = rows.map((r) => {
        const on = r.repo === dv.repo;
        return h('button', { type: 'button', role: 'tab', class: 'tab' + (on ? ' on' : ''), 'aria-selected': on ? 'true' : 'false', tabindex: on ? '0' : '-1',
          onclick: () => { if (!on) dv.select(r.repo, firstScope(r)); } },
          r.review ? svg((PR_STATES[r.review.state] || PR_STATES.open).icon, 'pricon ' + (r.review.state || 'open')) : null,
          r.repo,
          r.change ? [h('b', { class: 'add' }, '+' + r.change.added), h('b', { class: 'del' }, '−' + r.change.removed)] : null);
      });
      const bar = h('div', { class: 'tabbar' },
        rows.length ? h('div', { role: 'tablist', 'aria-label': 'Repositories', class: 'tabs', onkeydown: arrows }, repoTabs)
          : st.error ? errorBox({ message: st.error }) : h('div', { class: 'tabs' }, skeleton('line w3')),
        h('span', { class: 'grow' }),
        h('span', { class: 'muted small', title: 'The last time the clones asked their remotes, which is how old what is measured against them can be.' },
          st.loading ? 'reading…' : st.fetched ? 'remotes read ' + ago(st.fetched) : ''),
        h('button', { type: 'button', class: 'btn small ghost', title: 'Ask the remotes, then read the branches and their requests again', onclick: () => loadCode(key, true, true) }, 'Reread'));

      if (!rows.length || !dv.repo) {
        barBox.replaceChildren(bar); topBox.replaceChildren(); treeBox.hidden = true; layout.classList.add('single');
        listBox.replaceChildren(h('div', null, skeleton('line w3'), skeleton('line w4')));
        return;
      }

      // What the repository is: its branch, its request and what has not moved
      // yet. What changed is in the tabs below, so the facts leave those out.
      const facts = factsOf(key, row, 'tabs');
      const scopes = [row.change && 'branch', row.uncommitted && 'uncommitted'].filter(Boolean);
      const scopeLabel = {
        branch: ['Changes against ' + (dv.base || row.base || 'its base'), row.change],
        uncommitted: ['Not committed', row.uncommitted],
      };
      const scopeTabs = scopes.length ? h('div', { role: 'tablist', 'aria-label': 'What to read', class: 'tabs sub', onkeydown: arrows }, scopes.map((sc) => {
        const on = dv.scope === sc;
        const n = scopeLabel[sc][1];
        return h('button', { type: 'button', role: 'tab', class: 'tab' + (on ? ' on' : ''), 'aria-selected': on ? 'true' : 'false', tabindex: on ? '0' : '-1',
          onclick: () => { if (!on) dv.select(dv.repo, sc); } },
          scopeLabel[sc][0], h('span', { class: 'muted' }, plural(n.files, 'file', 'files')),
          h('b', { class: 'add' }, '+' + n.added), h('b', { class: 'del' }, '−' + n.removed));
      })) : null;

      let list;
      if (dv.error) list = errorBox({ message: dv.error });
      else if (!scopes.length) list = h('p', { class: 'muted' }, 'Nothing changed in ' + dv.repo + (row.base ? ' against ' + row.base : '') + '.');
      else if (!dv.files) list = h('div', null, skeleton('line w3'), skeleton('line w4'), skeleton('line w2'));
      else if (!dv.files.length) list = h('p', { class: 'muted' }, 'Nothing changed.');
      else list = h('ul', { class: 'cfiles' }, dv.files.map((f) => fileRow(f)));

      barBox.replaceChildren(bar);
      topBox.replaceChildren(h('div', { role: 'tabpanel', class: 'tabpanel' },
        h('div', { class: 'repo-head' }, row.branch ? h('span', { class: 'chip branch', title: 'The branch checked out in this repository' }, row.branch) : null,
          reviewOf(row), ...facts),
        scopeTabs,
        scopes.length && dv.files && dv.files.length > 1 ? h('p', { class: 'muted small tip' }, 'Opening a file closes the others, unless you pin it with “Keep open”.') : null));
      const navigable = !!(dv.files && dv.files.length > 1);
      treeBox.hidden = !navigable;
      layout.classList.toggle('single', !navigable);
      listBox.replaceChildren(list);
      if (navigable) paintTree();
    }

    // The files as folders, the way GitHub and GitLab list them: a folder that
    // holds only another folder is written as one path, and a filter shows what
    // matches wherever it is.
    function paintTree() {
      const files = (dv.files || []).filter((f) => !dv.filter || f.path.toLowerCase().includes(dv.filter));
      const root = { dirs: new Map(), files: [] };
      for (const f of files) {
        const parts = f.path.split('/');
        let node = root;
        for (const part of parts.slice(0, -1)) {
          if (!node.dirs.has(part)) node.dirs.set(part, { dirs: new Map(), files: [] });
          node = node.dirs.get(part);
        }
        node.files.push({ name: parts[parts.length - 1], f });
      }
      const draw1 = (node, path) => {
        const items = [];
        for (let [name, dir] of [...node.dirs.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
          let full = path ? path + '/' + name : name;
          while (dir.dirs.size === 1 && !dir.files.length) { // a path of folders, shown as one
            const [[n, d]] = dir.dirs.entries();
            name += '/' + n; full += '/' + n; dir = d;
          }
          const shut = !dv.filter && dv.collapsed.has(full);
          items.push(h('li', null,
            h('button', { type: 'button', class: 'fdir', 'aria-expanded': shut ? 'false' : 'true', title: full, onclick: () => {
              if (dv.collapsed.has(full)) dv.collapsed.delete(full); else dv.collapsed.add(full);
              paintTree();
            } }, h('span', { class: 'caret' }, shut ? '▸' : '▾'), h('span', { class: 'fname' }, name)),
            shut ? null : draw1(dir, full)));
        }
        for (const { name, f } of node.files.sort((a, b) => a.name.localeCompare(b.name))) {
          const cls = 'fnode' + (dv.active === f.path ? ' active' : '') + (dv.open.has(f.path) ? ' open' : '');
          items.push(h('li', null, h('button', { type: 'button', class: cls, title: f.oldPath ? f.oldPath + ' \u2192 ' + f.path : f.path, onclick: () => dv.show(f.path, true) },
            h('span', { class: 'fmark ' + f.status }, STATUS_MARK[f.status] || '?'), h('span', { class: 'fname' }, name),
            f.binary ? null : h('span', { class: 'fstat' }, h('b', { class: 'add' }, '+' + f.added), h('b', { class: 'del' }, '−' + f.removed)))));
        }
        return h('ul', null, items);
      };
      treeList.replaceChildren(files.length ? draw1(root, '') : h('p', { class: 'muted small' }, 'No file matches.'));
    }

    function fileRow(f) {
      const shown = dv.open.has(f.path), pinned = dv.pinned.has(f.path);
      return h('li', { class: shown ? 'open' : '', 'data-path': f.path },
        h('div', { class: 'cfile-row' },
          h('button', { type: 'button', class: 'cfile', 'aria-expanded': shown ? 'true' : 'false', onclick: () => dv.toggle(f.path) },
            h('span', { class: 'caret' }, shown ? '▾' : '▸'),
            h('span', { class: 'fmark ' + f.status, title: f.status }, STATUS_MARK[f.status] || '?'),
            h('span', { class: 'fpath mono' }, f.oldPath ? f.oldPath + ' → ' + f.path : f.path),
            f.binary ? h('span', { class: 'muted small' }, 'binary') : [h('b', { class: 'add' }, '+' + f.added), h('b', { class: 'del' }, '−' + f.removed)]),
          shown ? h('button', { type: 'button', class: 'pin' + (pinned ? ' on' : ''), 'aria-pressed': pinned ? 'true' : 'false',
            title: pinned ? 'Pinned: stays open when another file is opened. Click to unpin.' : 'Keep this one open when another file is opened.',
            onclick: () => dv.pin(f.path) }, pinned ? 'Pinned' : 'Keep open') : null),
        shown ? patchBox(dv.patches.get(f.path)) : null);
    }

    // The header is the universe as the fleet lists it, so it follows the fleet.
    const paintHead = (data) => {
      const w = ((data || {}).multiverses || []).find((x) => x.name === multiverse);
      const u = w && w.universes.find((x) => x.name === name);
      if (!u) { head.replaceChildren(h('h1', null, name), h('p', { class: 'muted' }, 'This universe is not in the fleet (any more).')); return; }
      const total = u.apps.reduce((n, a) => n + a.expected, 0);
      head.replaceChildren(
        h('div', { class: 'uni-head' }, h('h1', null, name), branchChip(u),
          badge(u.running ? 'ok' : 'mute', u.running ? u.running + (total ? '/' + total : '') + ' running' : 'stopped'),
          u.behind ? badge('warn', u.behind + ' behind') : null,
          h('span', { class: 'grow' }),
          h('button', { type: 'button', class: 'btn small', title: 'Containers, stats and logs', onclick: () => sidePanel.open(multiverse, name) }, 'Details')),
        h('p', { class: 'muted' }, [u.created && 'created ' + ago(u.created), u.lastStarted && 'started ' + ago(u.lastStarted), u.domain].filter(Boolean).join(' · ')),
        u.note ? h('p', { class: 'note' }, u.note) : null);
    };
    paintHead(state.fleet);
    loadCode(key, true);
    draw();
    if (repo) dv.select(repo, SCOPES[scope] ? scope : 'branch');
    return {
      title: name,
      onFleet(d) { paintHead(d); },
      stop() { if (diffPage === dv) diffPage = null; },
      async refresh() {
        if (!state.fleet) { try { paintHead(await api('fleet?' + query({ multiverse }))); } catch (_) { /* the header waits for the socket */ } }
      },
    };
  },
};

// A diff is text, drawn line by line with the mark it starts with, never as
// markup: it is a stranger's code.
function patchBox(p) {
  if (!p) return h('div', { class: 'patch' }, skeleton('line w4'));
  if (p.error) return h('div', { class: 'patch' }, errorBox({ message: p.error }));
  // Everything before the first hunk is the header; after it a line is what its
  // first character says, whatever it looks like — removing the comment
  // “-- drop table” is the line “--- drop table”, and is a removal.
  let inHunk = false;
  const kind = (l) => {
    if (l.startsWith('@@')) { inHunk = true; return 'hunk'; }
    if (!inHunk) return 'pmeta';
    if (l.startsWith('+')) return 'ins';
    if (l.startsWith('-')) return 'rem';
    if (l.startsWith('\\')) return 'pmeta';
    return '';
  };
  const lines = (p.patch || '').split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return h('div', { class: 'patch mono', tabindex: '0' },
    lines.map((l) => h('div', { class: 'pl ' + kind(l) }, l || ' ')),
    p.truncated ? h('div', { class: 'pl pmeta' }, '… cut: the rest of this diff is not shown here.') : null);
}

const PR_STATES = {
  open: { icon: 'prOpen', label: 'open', word: 'Open' },
  draft: { icon: 'prDraft', label: 'draft', word: 'Draft' },
  merged: { icon: 'prMerged', label: 'merged', word: 'Merged' },
  closed: { icon: 'prClosed', label: 'closed', word: 'Closed' },
};

// Only a web address becomes a link, as everywhere else on this page.
function reviewOf(r) {
  const v = r.review;
  if (v && /^https?:\/\//.test(v.url)) {
    const state = PR_STATES[v.state] || PR_STATES.open;
    const kind = v.mark === '!' ? 'merge request' : 'pull request';
    return h('a', { class: 'chip pr ' + (v.state || 'open'), href: v.url, target: '_blank', rel: 'noopener noreferrer',
      title: [state.word + ' ' + kind, v.title, v.author && 'opened by ' + v.author, v.updated && 'moved ' + ago(v.updated)].filter(Boolean).join(' · ') },
      svg(state.icon, 'pricon'), h('b', null, v.mark + v.number),
      h('span', { class: 'prstate' }, state.label),
      h('span', { class: 'prtitle' }, v.title));
  }
  // No chip is the answer when the branch never had a request: a line saying so on
  // every repository of every universe would drown the ones that have one.
  if (r.reviewError) return badge('warn', 'unanswered', 'The hosting service could not be asked: ' + r.reviewError);
  return null;
}

// What a doctor run said, kept by what it ran on, because the fleet is redrawn
// whenever something changes and a result should not vanish with it.
const doctorResults = new Map();

async function runDoctor(into, multiverse, universe, btn) {
  btn.disabled = true;
  const key = multiverse + '/' + universe;
  into.replaceChildren(h('p', { class: 'muted' }, 'Checking…'));
  try {
    const rep = await api('doctor?' + query(universe ? { multiverse, universe } : { multiverse }));
    const kind = { ok: 'ok', warn: 'warn', fail: 'bad' };
    const result = h('div', { class: 'panel' },
      h('div', { class: 'ws-head tight' }, h('b', null, rep.ok ? 'No failures' : 'Something failed'), h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'btn small ghost', onclick: () => { doctorResults.delete(key); result.remove(); } }, 'Dismiss')),
      h('ul', { class: 'checks' }, rep.checks.map((c) => h('li', null,
        badge(kind[c.status] || 'mute', c.status), h('span', { class: 'mono' }, c.name), h('span', null, c.detail || '')))));
    doctorResults.set(key, result);
    into.replaceChildren(result);
  } catch (e) { into.replaceChildren(errorBox(e)); }
  finally { btn.disabled = false; }
}

views.fleet = {
  title: 'Fleet',
  mount(host) {
    let filter = '', data = null;
    const body = h('div');
    const search = h('input', {
      type: 'search', name: 'filter', placeholder: 'Filter universes by name, branch or note   ( / )', 'aria-label': 'Filter the fleet',
      oninput: (e) => { filter = e.target.value.trim().toLowerCase(); draw(); },
    });
    host.append(h('h1', null, 'Fleet'),
      h('p', { class: 'lede' }, 'Every universe of every installed multiverse, and what is running in it.'),
      h('div', { class: 'bar' }, search), body);
    const wanted = (u) => !filter || [u.name, u.note, ...Object.values(u.branches || {})].some((s) => (s || '').toLowerCase().includes(filter));

    function draw() {
      if (!data) return;
      if (data.error) { body.replaceChildren(errorBox(data.error)); return; }
      if (!data.multiverses.length) {
        body.replaceChildren(empty('Nothing installed yet.', h('br'), cmd('multiversectl start <owner/repo>')));
        return;
      }
      // The cards about to be thrown away are no longer worth watching, and an
      // observer keeps hold of what it watches: the fleet is redrawn whenever
      // anything on the machine moves.
      if (codeWatch) codeWatch.disconnect();
      body.replaceChildren(...data.multiverses.map((w) => {
        const list = w.universes.filter(wanted);
        const panel = h('div');
        return h('section', null,
          h('div', { class: 'ws-head' }, h('h2', null, h('a', { href: '#/multiverse/' + encodeURIComponent(w.name) }, w.name)),
            h('span', { class: 'muted' }, w.universes.length + (w.universes.length === 1 ? ' universe' : ' universes')),
            h('span', { class: 'grow' }),
            wsActions(w, { create: true, update: true }, w),
            h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, '', e.target) }, 'Doctor')),
          panel,
          w.error ? errorBox(w.error) : null,
          (w.warnings || []).map((x) => h('p', { class: 'muted' }, badge('warn', 'warning'), ' ', x)),
          list.length ? h('div', { class: 'universes' }, list.map((u) => universeCard(w, u)))
            : (!w.error && h('div', { class: 'empty' }, w.universes.length ? 'No universe matches.' : ['No universes yet. ', cmd('multiversectl -m ' + w.name + ' universe new 1')])));
      }));
    }
    return {
      async refresh() {
        try { data = state.fleet && state.connected ? state.fleet : await api('fleet'); draw(); }
        catch (e) { body.replaceChildren(errorBox(e)); }
      },
      onFleet(d) { data = d; draw(); },
      onOps() { draw(); },
      onMode() { draw(); },
      focus: () => search.focus(),
    };
  },
};

views.multiverse = {
  title: 'Multiverse',
  mount(host, name) {
    const body = h('div');
    host.append(h('p', null, h('a', { href: '#/' }, '← Overview')), h('h1', null, name), body);
    const panel = h('div');
    let loaded = false;
    return {
      onOps() { if (loaded) this.refresh(); },
      onMode() { if (loaded) this.refresh(); },
      async refresh() {
        try {
          loaded = true;
          const w = await api('multiverses/' + encodeURIComponent(name));
          body.replaceChildren(
            h('p', { class: 'lede' }, copyable(w.root)),
            state.readOnly ? null : h('div', { class: 'bar' }, wsActions({ name: w.name, universes: [] }, { create: true, update: true })),
            h('div', { class: 'cards' },
              h('div', { class: 'card' }, h('h3', null, 'Reach'), h('div', null, 'domain ', h('b', null, w.domain), ' · ', w.expose || 'default')),
              h('div', { class: 'card' }, h('h3', null, 'Proxy'), h('div', { class: 'mono' }, w.network),
                h('div', null, Object.entries(w.entrypoints || {}).map(([k, v]) => [h('span', { class: 'chip' }, k + ' :' + v), ' '])))),
            h('h2', null, 'Repositories'),
            h('div', { class: 'scroll' }, h('table', null,
              h('thead', null, h('tr', null, ['Name', 'Repository', 'Clone'].map((t) => h('th', null, t)))),
              h('tbody', null, w.repos.map((r) => h('tr', null, h('td', null, r.key), h('td', null, place(r.loc) || h('span', { class: 'mono' }, r.url)),
                h('td', null, r.cloned ? badge('ok', 'cloned') : badge('warn', 'not cloned'), r.behind ? badge('warn', r.behind + ' behind ' + r.follows, 'The shared clone lacks these commits; “Update” on the multiverse brings them in.') : null)))))),
            h('h2', null, 'Apps'),
            h('div', { class: 'cards' }, w.apps.map((a) => h('div', { class: 'card' }, h('h3', null, a.key),
              h('div', null, 'repo ', h('b', null, a.repo)),
              a.services && a.services.length ? h('div', { class: 'svcs' }, a.services.map((s) => h('span', { class: 'svc' }, s))) : null,
              a.optional && a.optional.length ? h('div', { class: 'muted' }, 'optional: ', a.optional.join(', ')) : null,
              (a.hosts || []).map((x) => h('div', { class: 'mono' }, x.name + ' → ' + x.service + ':' + x.port + (x.entrypoint !== 'web' ? ' (' + x.entrypoint + ')' : '')))))),
            h('div', { class: 'ws-head' }, h('h2', null, 'Health'),
              h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, name, '', e.target) }, 'Run doctor')),
            panel,
            h('p', null, h('a', { href: '#/fleet' }, 'See its universes in the fleet →')));
        } catch (e) { body.replaceChildren(errorBox(e)); }
      },
    };
  },
};

// ── actions ────────────────────────────────────────────────────────────
// Everything the page can do is a use case the command line has too. The
// server runs it in the background and sends its output as it happens; the
// dock at the bottom is where that is read.

const isBusy = (multiverse) => state.ops.some((o) => o.multiverse === multiverse && o.state === 'running');

// run starts an action and opens the dock on it. An error comes back to the
// caller, which is a dialog that can say it where the person is looking.
async function run(request) {
  const op = await post('ops', request);
  upsertOp(op);
  state.shown = op.id;
  state.dockOpen = true;
  renderDock();
  return op;
}

async function runSafely(request) {
  try { await run(request); } catch (e) { toast(e.message); }
}

// The buttons that act on a whole multiverse. A multiverse row may not know its
// universes, so the caller can pass the fleet entry that does.
function wsActions(w, want, fleetEntry) {
  if (state.readOnly) return null;
  const busy = isBusy(w.name);
  const tip = busy ? 'Another operation is running in ' + w.name : null;
  return h('span', { class: 'actions' },
    want.create ? h('button', { type: 'button', class: 'btn small primary', disabled: busy, title: tip, onclick: () => newDialog(w) }, 'New universe') : null,
    want.update ? h('button', { type: 'button', class: 'btn small', disabled: busy, title: tip || 'Fetch the manifest and clone what it newly lists', onclick: () => runSafely({ action: 'update', multiverse: w.name }) }, 'Update') : null);
}

// modal shows a dialog with a form. submit may throw, and what it says is shown
// inside the dialog; ctl lets it change the button.
function modal({ title, body, confirm, danger, submit, ready }) {
  const dlg = h('dialog', { class: 'modal', 'aria-label': title });
  const err = h('div', { class: 'error', role: 'alert', hidden: true });
  const ok = h('button', { type: 'submit', class: 'btn ' + (danger ? 'danger' : 'primary') }, confirm);
  const ctl = { dlg, ok, error: (m) => { err.textContent = m; err.hidden = false; }, refresh: () => { ok.disabled = ready ? !ready() : false; } };
  const form = h('form', {
    method: 'dialog',
    onsubmit: async (e) => {
      e.preventDefault();
      ok.disabled = true; err.hidden = true;
      try { await submit(ctl); dlg.close(); } catch (x) { ctl.error(x.message || String(x)); ctl.refresh(); }
    },
  }, h('h2', null, title), body, err,
  h('div', { class: 'modal-actions' }, h('button', { type: 'button', class: 'btn ghost', onclick: () => dlg.close() }, 'Cancel'), ok));
  dlg.append(form);
  dlg.addEventListener('close', () => dlg.remove());
  document.body.append(dlg);
  ctl.refresh();
  dlg.showModal();
  return ctl;
}

function removeDialog(w, u) {
  let force = false;
  const input = h('input', { type: 'text', name: 'confirm', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', placeholder: u.name,
    'aria-label': 'Type the universe name to confirm', oninput: () => ctl.refresh() });
  const ctl = modal({
    title: 'Remove universe ' + u.name + '?', danger: true, confirm: 'Remove',
    ready: () => input.value === u.name,
    body: [
      h('p', null, 'This deletes the universe’s worktrees, so uncommitted work in them is lost, its containers and its volumes, which hold its data, and its state. The shared clones and their branches are not touched.'),
      h('label', { class: 'field' }, h('span', null, 'Type ', h('b', { class: 'mono' }, u.name), ' to confirm'), input),
    ],
    async submit(c) {
      try {
        await run({ action: 'rm', multiverse: w.name, universe: u.name, confirm: input.value, force });
      } catch (e) {
        // Uncommitted work is never discarded by accident: the server refuses,
        // says where it is, and only a second, deliberate press removes it.
        if (e.status === 409 && e.body && e.body.dirty) {
          force = true;
          c.ok.textContent = 'Remove anyway';
          throw new Error(e.message + '. Press the button again to discard it.');
        }
        throw e;
      }
    },
  });
  input.focus();
}

function newDialog(w) {
  // Two ways to make a universe, and they never mix: a fresh one starts branches
  // that exist nowhere, so it cannot land on anyone's work; a checkout one works
  // on a branch that already exists.
  let mode = 'new';
  const mk = (name, value, extra) => h('input', { type: 'text', name, value: value || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', ...extra });
  const name = mk('name', '', { placeholder: 'next free number', 'aria-label': 'Number or name', oninput: () => ctl.refresh() });
  const base = mk('base', '', { placeholder: 'default: each repository’s default branch', 'aria-label': 'Base branch' });
  const newBranch = mk('branch', '', { placeholder: 'default: the number or name', 'aria-label': 'Name of the new branch' });
  const pick = mk('pick', '', { list: 'branch-list', placeholder: 'start typing a branch', 'aria-label': 'Branch to check out', oninput: () => { hint(); ctl.refresh(); } });
  const pickName = mk('pickname', '', { placeholder: 'default: the branch', 'aria-label': 'Universe name' });
  const only = h('input', { type: 'checkbox', name: 'only', 'aria-label': 'Leave repositories without the branch on their base' });
  const note = mk('note', '', { placeholder: 'why it exists, shown beside it', 'aria-label': 'Note' });
  const list = h('datalist', { id: 'branch-list' });
  const skipped = h('small', { class: 'muted' });
  const where = h('small', { class: 'muted' });
  let branches = [];

  const radio = (value, label, hint) => h('label', { class: 'radio' },
    h('input', { type: 'radio', name: 'mode', value, checked: value === mode, onchange: () => { mode = value; swap(); } }),
    h('span', null, h('b', null, label), h('small', { class: 'muted' }, hint)));
  const fresh = h('div', null,
    h('label', { class: 'field' }, h('span', null, 'Number or name'), name, skipped),
    h('label', { class: 'field' }, h('span', null, 'Start from (optional)'), base),
    h('label', { class: 'field' }, h('span', null, 'Name of the new branch (optional)'), newBranch));
  const existing = h('div', { hidden: true },
    h('label', { class: 'field' }, h('span', null, 'Branch to check out'), pick, list, where),
    h('label', { class: 'field' }, h('span', null, 'Name the universe (optional)'), pickName),
    h('label', { class: 'check' }, only, h('span', null, 'Leave repositories that lack it on their base branch, detached, instead of starting a branch there')));

  function swap() { fresh.hidden = mode !== 'new'; existing.hidden = mode !== 'checkout'; ctl.refresh(); (mode === 'new' ? name : pick).focus(); }
  function hint() {
    const b = branches.find((x) => x.name === pick.value.trim());
    where.textContent = !pick.value.trim() ? '' : b ? 'exists in ' + b.repos.join(', ') : 'no repository has this branch';
  }

  const ctl = modal({
    title: 'New universe in ' + w.name, confirm: 'Create',
    ready: () => mode === 'new' || pick.value.trim() !== '',
    body: [
      h('div', { class: 'modes', role: 'radiogroup', 'aria-label': 'How to start' },
        radio('new', 'A fresh branch', 'Starts branches that do not exist yet, so it cannot clash with any'),
        radio('checkout', 'An existing branch', 'Checks out a branch that is already there, to work on or review it')),
      fresh, existing,
      h('label', { class: 'field' }, h('span', null, 'Note (optional)'), note),
    ],
    async submit() {
      if (mode === 'new') {
        await run({ action: 'new', multiverse: w.name, name: name.value.trim(), base: base.value.trim(), branch: newBranch.value.trim(), note: note.value.trim() });
      } else {
        await run({ action: 'checkout', multiverse: w.name, branch: pick.value.trim(), name: pickName.value.trim(), onlyExisting: only.checked, note: note.value.trim() });
      }
    },
  });

  // What the server knows that a form cannot: the number that is free (not just
  // one no universe has) and the branches there are.
  api('next?multiverse=' + encodeURIComponent(w.name)).then((n) => {
    if (!name.value) name.value = String(n.number);
    if (n.skipped && n.skipped.length) skipped.textContent = 'Skipped ' + n.skipped.join('; ') + '.';
    ctl.refresh();
  }).catch(() => {});
  api('branches?multiverse=' + encodeURIComponent(w.name)).then((r) => {
    branches = r.branches || [];
    list.replaceChildren(...branches.map((b) => h('option', { value: b.name }, b.repos.join(', '))));
    hint();
  }).catch(() => {});
  name.select();
}

// ── the operations dock ────────────────────────────────────────────────

function upsertOp(op) {
  const i = state.ops.findIndex((o) => o.id === op.id);
  if (i < 0) state.ops.push({ ...op, lines: op.lines || [] });
  else state.ops[i] = { ...op, lines: op.lines && op.lines.length ? op.lines : state.ops[i].lines };
  if (state.ops.length > 30) state.ops.splice(0, state.ops.length - 30);
}

const OP_STATE = { running: ['info', 'running'], done: ['ok', 'done'], failed: ['bad', 'failed'] };
let dockLog = null;

function renderDock() {
  const dock = document.getElementById('dock');
  if (!state.ops.length) { dock.hidden = true; return; }
  dock.hidden = false;
  const op = state.ops.find((o) => o.id === state.shown) || state.ops[state.ops.length - 1];
  const [kind, label] = OP_STATE[op.state] || ['mute', op.state];
  const running = state.ops.filter((o) => o.state === 'running').length;
  const head = h('div', { class: 'dock-head' },
    op.state === 'running' ? h('span', { class: 'spin', 'aria-hidden': 'true' }) : null,
    badge(kind, label), h('b', null, op.title),
    h('span', { class: 'muted' }, running > 1 ? running + ' running' : ''),
    h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'btn small ghost', 'aria-expanded': String(state.dockOpen),
      onclick: () => { state.dockOpen = !state.dockOpen; renderDock(); } }, state.dockOpen ? 'Hide' : 'Show log'));
  const kids = [head];
  if (state.dockOpen) {
    dockLog = h('pre', { class: 'log mono', tabindex: '0', 'aria-label': 'Output of ' + op.title });
    for (const l of op.lines) dockLog.append(logLine(l));
    if (op.error) dockLog.append(logLine({ kind: 'err', text: op.error }));
    kids.push(dockLog);
    if (state.ops.length > 1) {
      kids.push(h('div', { class: 'dock-ops' }, state.ops.slice().reverse().map((o) => h('button', {
        type: 'button', class: 'chip' + (o.id === op.id ? ' on' : ''), onclick: () => { state.shown = o.id; renderDock(); },
      }, [o.state === 'failed' ? '✗ ' : o.state === 'done' ? '✓ ' : '… ', o.title]))));
    }
    queueMicrotask(() => { dockLog.scrollTop = dockLog.scrollHeight; });
  } else {
    dockLog = null;
  }
  dock.replaceChildren(...kids);
}

const logLine = (l) => h('span', { class: 'l ' + l.kind }, l.text + '\n');

function onOpLine(d) {
  const op = state.ops.find((o) => o.id === d.id);
  if (!op) return;
  op.lines.push({ kind: d.kind, text: d.text });
  if (op.lines.length > 3000) op.lines.shift();
  if (dockLog && (state.shown || state.ops[state.ops.length - 1].id) === d.id) {
    const stick = dockLog.scrollTop + dockLog.clientHeight >= dockLog.scrollHeight - 24;
    dockLog.append(logLine(d));
    if (stick) dockLog.scrollTop = dockLog.scrollHeight;
  }
}

function onOp(op) {
  const before = state.ops.find((o) => o.id === op.id);
  upsertOp(op);
  if (!state.shown || !before) state.shown = op.id;
  if (!before && op.state === 'running') state.dockOpen = true;
  renderDock();
  if (before && before.state === 'running' && op.state !== 'running') {
    toast(op.state === 'done' ? op.title + ': done' : op.title + ': failed');
    sidePanel.reload();
  }
  if (current && current.onOps) current.onOps();
}

// ── the live connection ────────────────────────────────────────────────

let socket = null, retries = 0;

function setLive(on) {
  state.connected = on;
  const el = document.getElementById('live');
  el.textContent = on ? 'live' : 'reconnecting…';
  el.className = 'live ' + (on ? 'on' : 'off');
  el.title = on ? 'Changes are pushed to this page as they happen' : 'The connection dropped; the page reloads its data every ten seconds until it is back';
  schedule();
}

function connect() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  socket = new WebSocket(proto + '//' + location.host + '/ws');
  socket.onopen = () => { retries = 0; setLive(true); };
  socket.onmessage = (m) => { let e; try { e = JSON.parse(m.data); } catch (_) { return; } onEvent(e); };
  socket.onclose = () => { setLive(false); setTimeout(connect, Math.min(10000, 1000 * 2 ** retries++)); };
}

function onEvent(e) {
  switch (e.type) {
    case 'hello': {
      state.hello = e.data;
      const was = state.readOnly;
      state.readOnly = !!e.data.readOnly;
      state.ops = (e.data.ops || []).map((o) => ({ ...o, lines: o.lines || [] }));
      showVersion();
      renderDock();
      if (was !== state.readOnly && current && current.onMode) current.onMode();
      break;
    }
    case 'fleet': state.fleet = e.data; if (current && current.onFleet) current.onFleet(e.data); stamp(); break;
    case 'overview': state.overview = e.data; if (current && current.onOverview) current.onOverview(e.data); stamp(); break;
    case 'update': state.update = e.data.notice || null; showUpdate(); break;
    case 'catalogProgress': if (current && current.onCatalogProgress) current.onCatalogProgress(e.data); break;
    case 'op': onOp(e.data); break;
    case 'opline': onOpLine(e.data); break;
    default: break;
  }
}

// A newer release than this server, and what it fixes, above everything: it is
// the one thing on the page that is about the tool and not about the fleet. The
// server looks every half hour and tells every page, so none has to ask.
function showUpdate() {
  const el = document.getElementById('update');
  const n = state.update;
  if (!n) { el.hidden = true; el.replaceChildren(); return; }
  const count = (k, one, many) => k + (n.more ? '+' : '') + ' ' + (k === 1 && !n.more ? one : many);
  const brings = n.fixes ? count(n.fixes, 'fix', 'fixes') : n.changes ? count(n.changes, 'change', 'changes') : '';
  const hl = n.highlights || [];
  const more = n.fixes - hl.length;
  const andMore = more > 0 ? 'and ' + more + (n.more ? '+' : '') + ' more' : n.more ? 'and older ones' : null;
  el.replaceChildren(
    h('div', { class: 'update-line' },
      h('b', null, 'Update available'),
      h('span', null, 'multiversectl ' + n.latest + ' is out, you are on ' + n.current + (brings ? ': ' + brings + ' since' : '') + '.'),
      copyable('multiversectl self-update'),
      h('a', { href: n.url, target: '_blank', rel: 'noopener noreferrer' }, 'Release notes')),
    hl.length ? h('details', { class: 'update-fixes' },
      h('summary', null, "What's fixed"),
      h('ul', null, hl.map((f) => h('li', null, f)), andMore ? h('li', { class: 'muted' }, andMore) : null)) : null);
  el.hidden = false;
}

function showVersion() {
  const hello = state.hello;
  if (!hello) return;
  const el = document.getElementById('version');
  el.textContent = hello.version;
  el.title = ['multiversectl-server ' + hello.version, hello.commit && 'commit ' + hello.commit, hello.date && 'built ' + hello.date].filter(Boolean).join(' · ');
  document.getElementById('railnote').textContent = state.readOnly
    ? 'Read-only: started with --read-only.'
    : 'Start, stop, create and remove universes here; the command line does the same.';
}

// ── stats ──────────────────────────────────────────────────────────────
// Every running container of every multiverse, read over and over and drawn as
// lines. The server keeps no history — one reading is all it has — so the
// window is what this page has seen since it was opened, and saying so is
// part of the view.

const STATS_WINDOWS = [[120000, '2 min'], [300000, '5 min'], [900000, '15 min']];
const STATS_EVERY = [[2000, 'every 2s'], [5000, 'every 5s'], [10000, 'every 10s'], [30000, 'every 30s']];
const MAX_SAMPLES = 600;

views.stats = {
  title: 'Stats',
  mount(host) {
    // What has been read, newest last: one entry per reading, each holding
    // every container that answered in it.
    const samples = [];
    let group = 'universe', span = 300000, every = 2000, paused = false;
    let timer = null, failed = null, busy = false;
    // Which universes are left out, rather than which are in: a universe that
    // appears while the page is open is shown without being asked for.
    const hidden = new Set();

    const colours = viz.palette();
    const root = h('div');
    const errors = h('div');
    const tiles = h('div', { class: 'cards tiles' });
    const charts = h('div', { class: 'charts' });
    const facets = h('div');
    const table = h('div');
    const note = h('span', { class: 'muted' });

    const pick = (label, options, value, onpick) => h('label', { class: 'pick' }, h('span', null, label),
      h('select', { 'aria-label': label, onchange: (e) => onpick(e.target.value) },
        options.map(([v, t]) => h('option', { value: v, selected: String(v) === String(value) }, t))));

    const multiverses = () => {
      const seen = new Set();
      for (const s of samples) for (const r of s.rows) seen.add(r.multiverse);
      return [...seen].sort();
    };

    // Every universe seen within the window, so one that has just stopped can
    // still be ticked off rather than disappearing from under the pointer.
    const universesSeen = () => {
      const seen = new Map();
      for (const x of samples) {
        if (x.t < Date.now() - span) continue;
        for (const r of x.rows) seen.set(uniOf(r), r);
      }
      return [...seen.entries()].sort((a, b) => a[0].localeCompare(b[0], undefined, { numeric: true }));
    };

    const shown = (r) => !hidden.has(uniOf(r));
    // The readings inside the window, with the universes that are ticked off
    // taken out: everything drawn goes through here, so the numbers agree.
    const windowed = () => samples.filter((x) => x.t >= Date.now() - span)
      .map((x) => ({ t: x.t, rows: x.rows.filter(shown) }));

    const pauseBtn = h('button', { type: 'button', class: 'btn', 'aria-pressed': 'false',
      onclick: () => { paused = !paused; pauseBtn.textContent = paused ? 'Resume' : 'Pause';
        pauseBtn.setAttribute('aria-pressed', String(paused)); pauseBtn.classList.toggle('primary', paused); arm(); draw(); } }, 'Pause');

    // Which universes to draw. It is a menu of its own because there can be
    // many of them and more than one is the usual answer.
    const uniLabel = h('span');
    const uniMenu = h('div', { class: 'menu' });
    const uniPick = h('details', { class: 'picker' },
      h('summary', { 'aria-label': 'Choose which universes to show' }, uniLabel),
      uniMenu);
    let uniShown = '';
    const refreshUniPick = () => {
      const all = universesSeen();
      const n = all.filter(([k]) => !hidden.has(k)).length;
      uniLabel.replaceChildren('Universes',
        h('b', null, !samples.length ? '…' : all.length && n === all.length ? 'all' : n + ' of ' + all.length));
      // The colour is part of what the menu shows, so it is part of what says
      // the menu has to be drawn again: the palette is claimed by the charts.
      const key = all.map(([k]) => k + ':' + colours.of(k)).join('\u0000') + '|' + [...hidden].sort().join('\u0000');
      if (key === uniShown) return;
      uniShown = key;
      const many = multiverses().length > 1;
      const set = (k, on) => { if (on) hidden.delete(k); else hidden.add(k); uniShown = ''; draw(); };
      uniMenu.replaceChildren(
        h('div', { class: 'menu-head' },
          h('button', { type: 'button', class: 'btn small ghost', onclick: () => { hidden.clear(); uniShown = ''; draw(); } }, 'All'),
          h('button', { type: 'button', class: 'btn small ghost', onclick: () => { all.forEach(([k]) => hidden.add(k)); uniShown = ''; draw(); } }, 'None')),
        ...all.map(([k, r]) => h('label', { class: 'menu-item' },
          h('input', { type: 'checkbox', checked: !hidden.has(k), onchange: (e) => set(k, e.target.checked) }),
          group === 'universe' ? h('span', { class: 'swatch ' + colours.of(k) }) : null,
          h('span', null, r.universe), many ? h('span', { class: 'muted' }, r.multiverse) : null)));
      if (!all.length) uniMenu.append(h('p', { class: 'muted menu-item' }, 'Nothing is running.'));
    };

    const bar = h('div', { class: 'bar' },
      uniPick,
      pick('Group by', [['universe', 'Universe'], ['app', 'App'], ['service', 'Container']], group, (v) => { group = v; uniShown = ''; draw(); }),
      pick('Window', STATS_WINDOWS, span, (v) => { span = Number(v); draw(); }),
      pick('Read', STATS_EVERY, every, (v) => { every = Number(v); arm(); }),
      pauseBtn, h('span', { class: 'grow' }), note);
    // A menu that stays open over the page is a menu in the way. The listener
    // is on the document, so it is this view's to take away again: a mount
    // that leaves one behind leaves its readings behind with it.
    const shut = (e) => { if (uniPick.open && !uniPick.contains(e.target)) uniPick.open = false; };
    document.addEventListener('click', shut);
    uniPick.addEventListener('keydown', (e) => { if (e.key === 'Escape') { uniPick.open = false; uniPick.querySelector('summary').focus(); } });

    host.append(h('h1', null, 'Stats'),
      h('p', { class: 'lede' }, 'What every running container of every universe is using, read live. ',
        'The history is this page’s own: it starts when the page opens and is lost when it closes.'),
      bar, root);
    root.append(errors, tiles, charts, facets, table);

    // ---- reading
    async function load() {
      if (busy) return;
      busy = true;
      try {
        const d = await api('stats');
        const t = Date.parse(d.at) || Date.now();
        samples.push({ t, rows: d.rows || [] });
        // Nothing older than the longest window is kept: it can never be drawn.
        const oldest = Date.now() - STATS_WINDOWS[STATS_WINDOWS.length - 1][0];
        while (samples.length && (samples[0].t < oldest || samples.length > MAX_SAMPLES)) samples.shift();
        failed = (d.sources || []).length ? d.sources : null;
        draw();
      } catch (e) {
        failed = [{ multiverse: 'this machine', error: e.message }];
        draw();
      } finally { busy = false; }
    }

    function arm() {
      clearInterval(timer);
      timer = paused ? null : setInterval(() => { if (!document.hidden) load(); }, every);
    }

    // ---- grouping
    // A universe is a multiverse and a name; a container is that and an app and a
    // service. Everything here files rows under one of the two.
    const uniOf = (r) => r.multiverse + '/' + r.universe;
    const svcOf = (r) => uniOf(r) + '/' + r.app + '/' + r.service;
    const keyOf = (r) => group === 'universe' ? uniOf(r) : group === 'app' ? uniOf(r) + '/' + r.app : svcOf(r);
    // With one multiverse its name adds nothing to every label; with two it is
    // what tells one multiverse's universe 1 from the other's.
    const labelOf = (r, many) => {
      const uni = (many ? r.multiverse + ' · ' : '') + r.universe;
      return group === 'universe' ? uni : group === 'app' ? uni + ' ' + r.app : uni + ' ' + r.app + '/' + r.service;
    };


    // series sums each group at each reading. Everything past the eighth group
    // is one line called “other”: a ninth colour nobody could tell apart would
    // be worse than saying “the rest”.
    function build(field) {
      const within = windowed();
      const labels = new Map();
      const order = [];
      const many = multiverses().length > 1;
      for (const s of within) {
        for (const r of s.rows) {
          const k = keyOf(r);
          if (!labels.has(k)) { labels.set(k, labelOf(r, many)); order.push(k); }
        }
      }
      const live = new Set(within.length ? within[within.length - 1].rows.map(keyOf) : []);
      // Those still running keep their colour first; a group that has gone
      // quiet does not take one from a group that is busy.
      colours.claim([...order].sort((a, b) => (live.has(b) ? 1 : 0) - (live.has(a) ? 1 : 0) || a.localeCompare(b)));
      const named = order.filter((k) => colours.has(k));
      const rest = order.filter((k) => !colours.has(k));
      const series = named.map((k) => ({ key: k, label: labels.get(k), cls: colours.of(k), points: [] }));
      if (rest.length) series.push({ key: '\u0000other', label: rest.length + ' more', cls: 'so', points: [], other: true });
      for (const s of within) {
        const sum = new Map();
        for (const r of s.rows) {
          const k = colours.has(keyOf(r)) ? keyOf(r) : '\u0000other';
          sum.set(k, (sum.get(k) || 0) + (r[field] || 0));
        }
        for (const line of series) line.points.push([s.t, sum.get(line.key) || 0]);
      }
      // Until the page has watched for a whole window, the plot shows what it
      // has: a line crammed against the right edge is not history.
      return { series, from: within.length ? Math.max(within[0].t, Date.now() - span) : Date.now() - span, to: Date.now() };
    }

    // ---- drawing
    function draw() {
      refreshUniPick();
      errors.replaceChildren(...(failed || []).map((f) => h('p', { class: 'error', role: 'alert' }, f.multiverse + ': ' + f.error)));
      const last = samples.length ? samples[samples.length - 1] : null;
      note.replaceChildren(paused ? badge('warn', 'paused') : badge('ok', 'reading ' + (every / 1000) + 's'),
        last ? ' last at ' + viz.clock(last.t) : ' waiting for the first reading');

      if (!last) { drawWaiting(); return; }
      root.removeAttribute('aria-busy');
      const rows = last.rows.filter(shown);
      const cpu = rows.reduce((n, r) => n + r.cpu, 0);
      const mem = rows.reduce((n, r) => n + (r.mem || 0), 0);
      const universes = new Set(rows.map(uniOf));
      const busiest = rows.slice().sort((a, b) => b.cpu - a.cpu)[0];

      tiles.replaceChildren(
        h('div', { class: 'card' }, h('h3', null, 'Running'), h('div', { class: 'big' }, rows.length),
          h('div', { class: 'muted' }, rows.length === 1 ? 'container' : 'containers', ' in ', plural(universes.size, 'universe', 'universes'))),
        h('div', { class: 'card' }, h('h3', null, 'CPU'), h('div', { class: 'big' }, viz.percent(cpu)),
          h('div', { class: 'muted' }, 'of one core, added up')),
        h('div', { class: 'card' }, h('h3', null, 'Memory'), h('div', { class: 'big' }, viz.bytes(mem)),
          h('div', { class: 'muted' }, 'resident, added up')),
        h('div', { class: 'card' }, h('h3', null, 'Busiest'), h('div', { class: 'big mono' }, busiest ? viz.percent(busiest.cpu) : '—'),
          h('div', { class: 'muted' }, busiest ? busiest.app + '/' + busiest.service + ' in ' + busiest.universe : 'nothing is running')));

      // An empty plot is a scale over nothing; say what to do instead.
      if (!windowed().some((s) => s.rows.length)) {
        charts.replaceChildren(empty(hidden.size
          ? 'Every universe is ticked off. Choose one above.'
          : ['Nothing is running on this machine. Start a universe in the ', h('a', { href: '#/fleet' }, 'fleet'), ' and its containers appear here.']));
        facets.replaceChildren();
        table.replaceChildren();
        return;
      }
      const c = build('cpu'), m = build('mem');
      charts.replaceChildren(
        plot('CPU', 'Percent of one core. A container on four cores can pass 100%.', c, viz.percent, 10),
        plot('Memory', 'Resident memory, added up.', m, viz.bytes, 1024));

      drawFacets(last);
      drawTable(last);
    }

    // The first reading takes a moment, and a reading that fails takes longer.
    // What is drawn meanwhile is the shape of the page that is coming, so the
    // tiles, the charts and the table do not drop into place one by one.
    function drawWaiting() {
      root.setAttribute('aria-busy', 'true');
      tiles.replaceChildren(...['Running', 'CPU', 'Memory', 'Busiest'].map((t) =>
        h('div', { class: 'card' }, h('h3', null, t), skeleton('big'), skeleton('line w3'))));
      charts.replaceChildren(...['CPU', 'Memory'].map((t) =>
        h('section', { class: 'card chart' },
          h('div', { class: 'chart-head' }, h('h3', null, t), skeleton('line w4')),
          skeleton('plot'))));
      facets.replaceChildren();
      table.replaceChildren(h('h2', null, 'Containers'),
        h('div', { class: 'skel-rows' }, [0, 1, 2, 3].map(() => h('div', { class: 'skel-row' },
          skeleton('line w2'), skeleton('line w3'), skeleton('line w1'), skeleton('line w3'), skeleton('line w1')))));
    }

    // plot is one chart with its legend and its hover readout. The legend is
    // always there: two lines must never be told apart by colour alone.
    function plot(title, why, data, format, base) {
      const readout = h('div', { class: 'readout', role: 'status' });
      const frame = h('div', { class: 'plot-frame' });
      const card = h('section', { class: 'card chart' },
        h('div', { class: 'chart-head' }, h('h3', null, title), h('span', { class: 'muted' }, why), h('span', { class: 'grow' }), readout),
        frame,
        h('div', { class: 'legend' }, data.series.map((s) => h('span', { class: 'key' + (s.other ? ' other' : '') },
          h('span', { class: 'swatch ' + s.cls }), s.label,
          h('b', { class: 'mono' }, format(s.points.length ? s.points[s.points.length - 1][1] : 0))))));
      const render = () => {
        const width = frame.clientWidth || card.clientWidth - 32 || 600;
        frame.replaceChildren(viz.chart(data.series, data.from, data.to, {
          width, height: 190, format, base, label: title,
          onhover: (hits, t) => {
            if (!hits) { readout.replaceChildren(); return; }
            const top = hits.slice().sort((a, b) => b.p[1] - a.p[1]).slice(0, 5);
            readout.replaceChildren(h('span', { class: 'muted' }, viz.clock(t)),
              ...top.map((x) => h('span', { class: 'hit' }, h('span', { class: 'swatch ' + x.s.cls }),
                h('b', null, format(x.p[1])), h('span', { class: 'muted' }, x.s.label))));
          },
        }));
      };
      queueMicrotask(render);
      return card;
    }

    // One small chart per universe, all on their own scale but side by side:
    // past a handful of lines in one plot, the answer is more plots.
    function drawFacets(last) {
      const by = new Map();
      const within = windowed();
      for (const s of within) {
        for (const r of s.rows) {
          const k = uniOf(r);
          if (!by.has(k)) by.set(k, { multiverse: r.multiverse, universe: r.universe, at: new Map() });
          const g = by.get(k);
          g.at.set(s.t, (g.at.get(s.t) || 0) + r.cpu);
        }
      }
      if (by.size < 2) { facets.replaceChildren(); return; }
      const from = within.length ? Math.max(within[0].t, Date.now() - span) : Date.now() - span;
      const now = Date.now();
      // One scale for all of them: side by side, their heights are compared.
      let peak = 0;
      for (const g of by.values()) for (const v of g.at.values()) if (v > peak) peak = v;
      const cards = [...by.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([k, g]) => {
        const points = [...g.at.entries()].sort((a, b) => a[0] - b[0]);
        const current = points.length ? points[points.length - 1][1] : 0;
        const live = last.rows.some((r) => uniOf(r) === k);
        const card = h('article', { class: 'card facet' },
          h('div', { class: 'facet-head' }, h('span', { class: 'dot ' + (live ? 'ok' : '') }), h('b', null, g.universe),
            h('span', { class: 'muted' }, g.multiverse), h('span', { class: 'grow' }), h('b', { class: 'mono' }, viz.percent(current))),
          h('div', { class: 'plot-frame' }));
        queueMicrotask(() => {
          const frame = card.querySelector('.plot-frame');
          frame.replaceChildren(viz.chart([{ key: k, label: g.universe, cls: colours.of(k), points }], from, now,
            { width: frame.clientWidth || 220, height: 70, format: viz.percent, compact: true, floor: peak, label: 'CPU of ' + g.universe }));
        });
        return card;
      });
      facets.replaceChildren(h('div', { class: 'ws-head tight' }, h('h2', null, 'Per universe'),
        h('span', { class: 'muted small' }, 'processor time, all on the same scale')),
        h('div', { class: 'facets' }, cards));
    }

    // The table is the chart's other half: every value a line holds is here as
    // a number, in an order that does not move while it is read.
    function drawTable(last) {
      const history = new Map();
      let from = Date.now();
      for (const s of windowed()) {
        from = Math.min(from, s.t);
        for (const r of s.rows) {
          const k = svcOf(r);
          if (!history.has(k)) history.set(k, []);
          history.get(k).push([s.t, r.cpu]);
        }
      }
      const rows = last.rows.filter(shown).sort((a, b) =>
        a.multiverse.localeCompare(b.multiverse) || a.universe.localeCompare(b.universe, undefined, { numeric: true })
        || a.app.localeCompare(b.app) || a.service.localeCompare(b.service));
      table.replaceChildren(h('h2', null, 'Containers'),
        rows.length ? h('div', { class: 'scroll' }, h('table', { class: 'stats-table' },
          h('thead', null, h('tr', null, ['Universe', 'Service', 'CPU', 'Last ' + (span / 60000) + ' min', 'Memory', 'Network', 'PIDs', ''].map((t) => h('th', null, t)))),
          h('tbody', null, rows.map((r) => {
            const k = svcOf(r);
            const cls = colours.of(keyOf(r));
            return h('tr', null,
              h('td', null, h('b', null, r.universe), ' ', h('span', { class: 'muted' }, r.multiverse)),
              h('td', null, h('span', { class: 'swatch ' + cls }), r.app + '/' + r.service),
              h('td', { class: 'mono num' }, viz.percent(r.cpu)),
              h('td', null, viz.sparkline(history.get(k) || [], from, Date.now(), cls, 110, 20)),
              h('td', { class: 'mono num', title: r.memUsage }, viz.bytes(r.mem || 0),
                r.memLimit ? h('span', { class: 'muted' }, ' ' + viz.percent(r.memory)) : null),
              h('td', { class: 'mono num muted' }, r.netIO || '—'),
              h('td', { class: 'mono num muted' }, r.pids || 0),
              h('td', { class: 'row-actions' },
                h('button', { type: 'button', class: 'btn small', onclick: () => sidePanel.open(r.multiverse, r.universe, { service: r.app + '/' + r.service }) }, 'Logs')));
          }))))
          : empty('Nothing is running. Start a universe in the ', h('a', { href: '#/fleet' }, 'fleet'), '.'));
    }

    let sized;
    const onResize = () => { clearTimeout(sized); sized = setTimeout(draw, 150); };
    addEventListener('resize', onResize);

    arm();
    // Drawn before anything has been read, so the view is the shape of itself
    // while it waits rather than an empty page that fills in all at once.
    draw();
    return {
      refresh: load,
      stop() { clearInterval(timer); removeEventListener('resize', onResize); document.removeEventListener('click', shut); },
    };
  },
};

// ── system ─────────────────────────────────────────────────────────────

views.system = {
  title: 'System',
  mount(host) {
    const body = h('div');
    host.append(h('h1', null, 'System'),
      h('p', { class: 'lede' }, 'The versions of what this machine runs on: this server, the command line, and the tools they drive.'), body);
    return {
      async refresh() {
        if (!body.firstChild) body.append(h('p', { class: 'muted' }, 'Checking the tools…'));
        try { body.replaceChildren(...system(await api('system'))); }
        catch (e) { body.replaceChildren(errorBox(e)); }
      },
    };
  },
};

function system(v) {
  const out = [];
  const srv = v.server || {};
  const cli = (v.tools || []).find((t) => t.name === 'multiversectl');
  if (v.latest && v.latest.newer) {
    out.push(h('div', { class: 'next' }, h('b', null, 'Update available'), 'multiversectl ' + v.latest.version + ' is out', copyable('multiversectl self-update')));
  }
  out.push(h('div', { class: 'cards' },
    h('div', { class: 'card' }, h('h3', null, 'This server'), h('div', { class: 'big mono' }, srv.version || '—'),
      h('div', { class: 'muted' }, [srv.commit && 'commit ' + srv.commit, srv.date && 'built ' + srv.date].filter(Boolean).join(' · ')),
      h('div', { class: 'muted' }, 'listening on ' + v.listen + ' · up since ' + ago(v.started).replace(' ago', '')),
      h('div', null, v.readOnly ? badge('warn', 'read-only') : badge('ok', 'actions enabled'), ' ', badge(state.connected ? 'ok' : 'warn', state.connected ? 'live' : 'reconnecting'))),
    h('div', { class: 'card' }, h('h3', null, 'Command line'), h('div', { class: 'big mono' }, cli && cli.ok ? cli.version : 'not found'),
      h('div', { class: 'muted' }, cli && cli.path ? cli.path : 'multiversectl is not beside this server or on the PATH')),
    h('div', { class: 'card' }, h('h3', null, 'Latest release'),
      h('div', { class: 'big mono' }, v.latest && v.latest.version ? v.latest.version : '—'),
      h('div', { class: 'muted' }, v.latest && v.latest.error ? v.latest.error : v.latest && v.latest.newer ? 'newer than the command line' : 'the command line is current')),
    h('div', { class: 'card' }, h('h3', null, 'File format'), h('div', { class: 'big mono' }, v.format || '—'),
      h('div', { class: 'muted' }, 'apiVersion of manifests and catalogs'))));
  out.push(h('h2', null, 'Tools'));
  out.push(h('div', { class: 'scroll' }, h('table', null,
    h('thead', null, h('tr', null, ['Tool', 'Version', 'Used for', 'Found at'].map((t) => h('th', null, t)))),
    h('tbody', null, (v.tools || []).map((t) => h('tr', null,
      h('td', null, h('b', null, t.name)),
      h('td', { class: 'mono' }, t.ok ? t.version : badge('warn', t.note || 'unknown')),
      h('td', { class: 'muted' }, t.ok ? t.note : ''),
      h('td', { class: 'mono' }, t.path || '—')))))));
  return out;
}

// ── shell ──────────────────────────────────────────────────────────────

let current = null, timer = null;

function route() {
  const [, page = '', arg, ...rest] = location.hash.split('/');
  const key = page === '' ? 'overview' : page;
  const view = views[key] || views.overview;
  document.querySelectorAll('.rail a').forEach((a) => {
    if (a.dataset.route === (key === 'multiverse' ? 'overview' : key === 'universe' ? 'fleet' : key)) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });
  if (current && current.stop) current.stop();
  main.replaceChildren();
  current = view.mount(main, arg ? decodeURIComponent(arg) : undefined, rest.map(decodeURIComponent));
  document.title = view.title + ' · multiversectl';
  refresh();
  main.focus({ preventScroll: true });
}

function stamp() { document.getElementById('stamp').textContent = 'updated ' + new Date().toLocaleTimeString(); }

async function refresh(fetch) {
  if (!current) return;
  await current.refresh(fetch);
  stamp();
}

// With the socket up, changes arrive on their own. Without it the page falls
// back to asking, so it still works behind something that drops websockets.
function schedule() {
  clearInterval(timer);
  if (state.connected) return;
  timer = setInterval(() => {
    if (!document.hidden && /^#\/(fleet|multiverse\/.*|universe\/.*)?$|^$/.test(location.hash)) refresh();
  }, REFRESH_MS);
}

function setTheme(t) {
  if (t) document.documentElement.dataset.theme = t; else delete document.documentElement.dataset.theme;
}
try { setTheme(localStorage.getItem('theme')); } catch (_) { /* private mode */ }

document.getElementById('theme').addEventListener('click', () => {
  const dark = document.documentElement.dataset.theme
    ? document.documentElement.dataset.theme === 'dark'
    : matchMedia('(prefers-color-scheme: dark)').matches;
  const next = dark ? 'light' : 'dark';
  setTheme(next);
  try { localStorage.setItem('theme', next); } catch (_) { /* private mode */ }
});
document.getElementById('refresh').addEventListener('click', (e) => {
  e.target.disabled = true;
  if (state.connected) socket.send(JSON.stringify({ type: 'refresh' }));
  refresh().finally(() => { e.target.disabled = false; });
});
document.addEventListener('keydown', (e) => {
  if (e.key === '/' && !/input|textarea/i.test(document.activeElement.tagName) && current && current.focus) { e.preventDefault(); current.focus(); }
});
window.addEventListener('hashchange', route);
route();
connect();
schedule();


// ── the side panel of one universe ─────────────────────────────────────
// Its containers with what they are using, and a log per service. It is
// outside what the fleet redraws, and everything in it is fetched when it
// opens and stopped when it closes, so a universe nobody is looking at costs
// nothing. Only the numbers are redrawn as they arrive: a row that is rebuilt
// under the pointer cannot be clicked or read.

const sidePanel = (() => {
  const root = document.getElementById('side');
  const STATS_MS = 3000;
  const MAX_LOG_LINES = 3000;
  const SPARK_POINTS = 40;
  let cur = null;
  let opener = null;
  // Wide is the panel over the page instead of beside it, which is what
  // reading a log wants: a terminal's width, and nothing moving behind it.
  // The choice is the reader's and it is remembered, since someone who reads
  // logs reads them again.
  let wide = false;
  try { wide = localStorage.getItem('side-wide') === '1'; } catch (_) { /* a private window is no reason to fail */ }
  const scrim = h('div', { class: 'scrim', hidden: true, onclick: close });
  document.body.append(scrim);

  function setWide(on) {
    wide = on;
    try { localStorage.setItem('side-wide', on ? '1' : '0'); } catch (_) { /* nothing is lost but the preference */ }
    applyWide();
    draw(); // the button says which way it goes next
  }

  // The page behind must not scroll under the panel: a wheel over a panel that
  // has reached its end should not carry on to the page below it.
  function applyWide() {
    const on = !!cur && wide;
    root.classList.toggle('wide', on);
    // Both, because a page scrolls from whichever of the two the browser
    // decided was the scrolling element.
    document.body.classList.toggle('side-over', on);
    document.documentElement.classList.toggle('side-over', on);
    scrim.hidden = !on;
  }

  const of = (c, extra) => query({ multiverse: c.multiverse, universe: c.universe, ...extra });

  function close() {
    if (!cur) return;
    stopLog();
    clearInterval(cur.statsTimer);
    cur = null;
    root.hidden = true;
    root.replaceChildren();
    document.body.classList.remove('with-side');
    applyWide();
    if (opener && opener.isConnected) opener.focus();
    opener = null;
  }

  // open(multiverse, universe, { service }) — with a service it opens straight on
  // that log, which is what a row elsewhere on the page links to.
  async function open(multiverse, universe, opts) {
    const want = (opts || {}).service;
    if (cur && cur.multiverse === multiverse && cur.universe === universe) {
      if (want) startLog(...want.split('/'));
      return;
    }
    const from = document.activeElement;
    close();
    opener = from;
    const c = { multiverse, universe, rows: null, stats: {}, history: new Map(), tab: 'containers', log: null, statsTimer: null, confirm: null, error: null };
    cur = c;
    root.hidden = false;
    document.body.classList.add('with-side');
    applyWide();
    draw();
    root.querySelector('.side-head h3').focus();
    await loadRows();
    // Closed, or another universe opened, while the containers were being
    // read: there is nothing left to poll for, and a timer started now would
    // be one nobody holds the handle of.
    if (cur !== c) return;
    if (want) startLog(...want.split('/'));
    pollStats();
    c.statsTimer = setInterval(pollStats, STATS_MS);
  }

  // reload is what an operation elsewhere calls: a service that was just
  // restarted or started should not still read “Exited” here.
  function reload() { if (cur) loadRows(); }

  async function loadRows() {
    const c = cur;
    try { const r = await api('universe/containers?' + of(c)); if (cur === c) { c.rows = r.containers; c.error = null; } }
    catch (e) { if (cur === c) c.error = e.message; }
    if (cur === c) draw();
  }

  async function pollStats() {
    const c = cur;
    if (!c || document.hidden || c.tab !== 'containers' || !(c.rows || []).some((r) => r.running)) return;
    try {
      const r = await api('universe/stats?' + of(c));
      if (cur !== c) return;
      c.stats = Object.fromEntries(r.stats.map((s) => [s.name, s]));
      const now = Date.now();
      for (const s of r.stats) {
        if (!c.history.has(s.name)) c.history.set(s.name, []);
        const points = c.history.get(s.name);
        points.push([now, s.cpu]);
        if (points.length > SPARK_POINTS) points.shift();
      }
      drawStats();
    } catch (_) { /* the next reading tries again */ }
  }

  // ---- logs
  function stopLog() { if (cur && cur.log && cur.log.abort) cur.log.abort.abort(); }

  async function startLog(appKey, service) {
    const c = cur;
    if (!c) return;
    const was = c.log;
    stopLog();
    const log = {
      appKey, service, lines: [], drawn: 0,
      tail: was ? was.tail : 200, follow: was ? was.follow : true, filter: was ? was.filter : '',
      wrap: was ? was.wrap : true, times: was ? was.times : true,
      abort: new AbortController(), state: 'loading', detail: '',
    };
    c.log = log;
    c.tab = 'logs';
    draw();
    try {
      const res = await fetch('/api/universe/logs?' + of(c, { app: appKey, service, tail: log.tail, follow: log.follow ? '1' : '0' }), { signal: log.abort.signal });
      if (!res.ok) { let b = {}; try { b = await res.json(); } catch (_) { /* empty */ } throw new Error(b.error || res.statusText); }
      log.state = log.follow ? 'following' : 'loaded';
      drawLogState();
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let rest = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        const text = rest + dec.decode(value, { stream: true });
        const parts = text.split('\n');
        rest = parts.pop();
        if (parts.length) { push(log, parts); drawLog(); }
      }
      if (rest) { push(log, [rest]); drawLog(); }
      log.state = 'ended';
      drawLogState();
    } catch (e) {
      if (e.name === 'AbortError') return;
      log.state = 'failed';
      log.detail = e.message;
      drawLogState();
    }
  }

  // A line as docker hands it over: an RFC 3339 stamp, a space, the rest. The
  // stamp is split off so it can be shown as a clock time, or not at all.
  const STAMP = /^(\d{4}-\d{2}-\d{2}T[\d:.]+Z?) (.*)$/s;
  // What a line looks like when it is bad news. Only a word on its own, near
  // the front, counts: "errors: 0" in the middle of a line is not an error.
  const LEVEL = [
    [/(^|[\s[|"'(])(error|fatal|critical|panic|exception|traceback)([\s\]:|"')]|$)/i, 'err'],
    [/(^|[\s[|"'(])(warn|warning|deprecated)([\s\]:|"')]|$)/i, 'warn'],
    [/(^|[\s[|"'(])(debug|trace)([\s\]:|"')]|$)/i, 'dim'],
  ];

  function push(log, raw) {
    for (const line of raw) {
      const m = STAMP.exec(line);
      const { text, parts } = ansi(m ? m[2] : line);
      let level = '';
      const head = text.slice(0, 160);
      for (const [re, name] of LEVEL) if (re.test(head)) { level = name; break; }
      log.lines.push({ at: m ? Date.parse(m[1]) : 0, text, parts, level });
    }
    if (log.lines.length > MAX_LOG_LINES) {
      const gone = log.lines.splice(0, log.lines.length - MAX_LOG_LINES);
      // The window moved: the oldest lines leave the view, and the ones
      // already drawn stay drawn. Rebuilding all three thousand on every
      // chunk would undo the incremental draw exactly when a log is busy
      // enough to need it.
      log.drawn -= gone.length;
      log.shed = (log.shed || 0) + gone.filter((l) => shows(log, l)).length;
    }
  }

  // docker hands a log over exactly as the program wrote it, escape codes and
  // all. A page that prints what it wrote shows “[32m” where the terminal
  // showed green, and a spinner as every frame it ever drew.
  //
  // So a line is built the way a terminal builds one: what the program painted
  // it in is kept, as classes rather than styles because the page's content
  // security policy allows no style of its own, and what moves the cursor,
  // erases or rewrites is carried out. A spinner then leaves the word it
  // finished on, which is the only one that was ever meant to be read.
  //
  // The groups are a control sequence's parameters and its final byte; after
  // it, an operating system command ended by a bell or a string terminator,
  // and last the two-byte escapes.
  const ANSI = /\x1b(?:\[([0-9;:?<=>]*)[ -/]*([@-~])|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[@-Z\\-_])/g;
  const FG = ['ac0', 'ac1', 'ac2', 'ac3', 'ac4', 'ac5', 'ac6', 'ac7'];
  const TAB = 8;
  // The furthest a program may send the cursor along a line it is writing.
  const MAX_COLS = 4096;

  function ansi(raw) {
    const on = { fg: '', bold: false, dim: false, italic: false, under: false };
    const cls = () => [on.fg, on.bold && 'ab', on.dim && 'ad', on.italic && 'ai', on.under && 'au'].filter(Boolean).join(' ');
    // One slot per column, so writing at a column that was written before
    // replaces what was there, as a terminal would. Writing is not capped —
    // a log line may be as long as it likes — but moving the cursor is, since
    // every column jumped over is a blank that has to be made and then read
    // back, and `ESC[5000000C` is a line nobody is going to read.
    const cells = [];
    let col = 0;
    const put = (ch) => { cells[col] = { ch, cls: cls() }; col++; };
    const plain = (run) => {
      for (const ch of run) {
        if (ch === '\r') col = 0;
        else if (ch === '\b') col = Math.max(0, col - 1);
        else if (ch === '\t') { do { put(' '); } while (col % TAB); }
        else if (ch >= ' ' || ch === '\u00a0') put(ch);
      }
    };

    let at = 0, m;
    ANSI.lastIndex = 0;
    while ((m = ANSI.exec(raw))) {
      if (m.index > at) plain(raw.slice(at, m.index));
      col = move(on, cells, col, m[1], m[2]);
      at = ANSI.lastIndex;
    }
    if (at < raw.length) {
      // An escape the line ends in the middle of has nothing to act on, and
      // the letters it was going to use are not text.
      const tail = raw.slice(at);
      const half = tail.indexOf('\u001b');
      plain(half < 0 ? tail : tail.slice(0, half));
    }

    // Trailing blanks are what erasing a longer line left behind. Counted
    // backwards over the columns: asking a regular expression where the
    // whitespace at the end of a line starts is quadratic in the length of
    // that whitespace, and a log line can carry a great deal of it.
    let end = cells.length;
    while (end > 0 && (!cells[end - 1] || cells[end - 1].ch === ' ')) end--;
    // A hole is a column the program skipped over; it reads as a space.
    const parts = [];
    let text = '';
    for (let i = 0; i < end; i++) {
      const c = cells[i] || { ch: ' ', cls: '' };
      const last = parts[parts.length - 1];
      if (last && last.cls === c.cls) last.s += c.ch;
      else parts.push({ s: c.ch, cls: c.cls });
      text += c.ch;
    }
    return { text, parts };
  }

  // What one control sequence does, as far as a log line is concerned: colour,
  // the cursor along its one line, and erasing. Anything that would need more
  // than a line — moving between them, scrolling, a saved position — cannot be
  // honoured here and is left out rather than half done.
  function move(on, cells, col, params, final) {
    const n = (i, or) => { const v = Number((params || '').split(';')[i]); return Number.isFinite(v) && (params || '').split(';')[i] !== '' ? v : or; };
    switch (final) {
      case 'm': paint(on, params); return col;
      case 'G': return Math.min(MAX_COLS, Math.max(0, n(0, 1) - 1));
      // A move of no columns is a move of one, which is what a terminal does
      // with a parameter of zero.
      case 'C': return Math.min(MAX_COLS, col + Math.max(1, n(0, 1)));
      case 'D': return Math.max(0, col - Math.max(1, n(0, 1)));
      case 'K': {
        // Erasing leaves the cursor where it is, however much it erases.
        const how = n(0, 0);
        if (how === 0) cells.length = col;
        else if (how === 1) for (let i = 0; i <= col; i++) cells[i] = { ch: ' ', cls: '' };
        else cells.length = 0;
        return col;
      }
      default: return col;
    }
  }

  // What one “ESC [ … m” does. Backgrounds are left alone: a log that paints
  // its own background fights the page it is read on, and the page wins.
  function paint(on, params) {
    const codes = (params || '0').split(';').map((x) => Number(x) || 0);
    for (let i = 0; i < codes.length; i++) {
      const n = codes[i];
      if (n === 0) { on.fg = ''; on.bold = on.dim = on.italic = on.under = false; }
      else if (n === 1) on.bold = true;
      else if (n === 2) on.dim = true;
      else if (n === 3) on.italic = true;
      else if (n === 4) on.under = true;
      else if (n === 22) on.bold = on.dim = false;
      else if (n === 23) on.italic = false;
      else if (n === 24) on.under = false;
      else if (n >= 30 && n <= 37) on.fg = FG[n - 30];
      else if (n >= 90 && n <= 97) on.fg = FG[n - 90];
      else if (n === 39) on.fg = '';
      else if (n === 38 || n === 48) {
        // An extended colour carries its own parameters, which are not codes.
        const taken = codes[i + 1] === 5 ? 2 : codes[i + 1] === 2 ? 4 : 1;
        if (n === 38 && codes[i + 1] === 5 && codes[i + 2] < 16) on.fg = FG[codes[i + 2] % 8];
        i += taken;
      }
    }
  }

  const shows = (log, l) => !log.filter || l.text.toLowerCase().includes(log.filter.toLowerCase());
  const visible = (log) => log.lines.filter((l) => shows(log, l));
  const asText = (log) => visible(log).map((l) => (log.times && l.at ? new Date(l.at).toISOString() + ' ' : '') + l.text).join('\n');

  // One line, in the colours the program wrote it in, with the part that
  // matched the filter marked in it.
  function lineEl(log, l) {
    const body = h('span', { class: 'lm' });
    for (const p of l.parts) {
      const kids = log.filter ? marked(p.s, log.filter) : [p.s];
      if (p.cls) body.append(h('span', { class: p.cls }, kids));
      else for (const k of kids) body.append(k instanceof Node ? k : document.createTextNode(k));
    }
    return h('div', { class: 'll ' + l.level },
      log.times && l.at ? h('span', { class: 'lt' }, new Date(l.at).toLocaleTimeString(undefined, { hour12: false })) : null,
      body);
  }

  // The filter is marked inside each coloured run of the line, not across the
  // line: a match that straddles a colour change would have to be split up to
  // be drawn anyway.
  function marked(s, f) {
    const out = [];
    const low = s.toLowerCase(), needle = f.toLowerCase();
    let i = 0;
    for (;;) {
      const at = low.indexOf(needle, i);
      if (at < 0) { if (i < s.length) out.push(s.slice(i)); break; }
      if (at > i) out.push(s.slice(i, at));
      out.push(h('mark', null, s.slice(at, at + f.length)));
      i = at + f.length;
    }
    return out;
  }

  // ---- drawing
  let rowsBody, logOut, logState, logCount, jump;

  function draw() {
    if (!cur) return;
    const c = cur;
    const tab = (id, label, count) => h('button', {
      type: 'button', role: 'tab', id: 'tab-' + id, 'aria-controls': 'tabpanel', class: 'tab' + (c.tab === id ? ' on' : ''),
      'aria-selected': c.tab === id ? 'true' : 'false',
      onclick: () => { c.tab = id; if (id === 'containers') { stopLog(); c.log = null; pollStats(); } draw(); },
    }, label, count != null ? h('span', { class: 'count' }, count) : null);
    const running = (c.rows || []).filter((r) => r.running).length;
    const uni = universeIn(c.multiverse, c.universe);
    root.replaceChildren(
      h('header', { class: 'side-head' },
        h('h3', { tabindex: '-1' }, c.universe),
        h('span', { class: 'muted' }, c.multiverse),
        c.rows ? badge(running ? 'ok' : 'mute', running ? running + ' running' : 'stopped') : null,
        h('span', { class: 'grow' }),
        h('button', {
          type: 'button', class: 'btn small ghost', 'aria-pressed': String(wide),
          title: wide ? 'Put it back beside the page' : 'Widen it over the page, for reading logs',
          'aria-label': wide ? 'Narrow the panel' : 'Widen the panel',
          onclick: () => setWide(!wide),
        }, wide ? 'Narrow' : 'Wide'),
        h('button', { type: 'button', class: 'btn small ghost close', 'aria-label': 'Close the panel (Esc)', title: 'Close (Esc)', onclick: close }, '✕')),
      // Named by the hostname, not by the service behind it: two hosts of one
      // app often share a service, and two chips both reading “web” say
      // nothing. These stay even when nothing runs, where the rows below are
      // the containers and there are none.
      uni && uni.apps ? h('div', { class: 'side-urls' }, uni.apps.flatMap((a) => (a.urls || []).map((x) =>
        h('a', { href: x.url, target: '_blank', rel: 'noopener noreferrer', class: 'chip', title: x.url + ' \u2192 ' + a.key + '/' + x.service },
          x.host || x.service || a.key)))) : null,
      h('div', { class: 'tabs', role: 'tablist' }, tab('containers', 'Containers', c.rows ? c.rows.length : null), tab('logs', 'Logs')),
      ...(c.error ? [errorBox(c.error)] : []),
      h('div', { id: 'tabpanel', role: 'tabpanel', 'aria-labelledby': 'tab-' + c.tab, class: 'side-body' },
        c.tab === 'containers' ? containersTab() : logsTab()));
  }

  // What the fleet already knows about this universe, when it knows it: the
  // panel does not ask again for what is on the page.
  function universeIn(multiverse, universe) {
    const w = ((state.fleet || {}).multiverses || []).find((x) => x.name === multiverse);
    return w && (w.universes || []).find((u) => u.name === universe);
  }

  // Each row is built once; only its numbers change as readings come in.
  let meterEls;

  function containersTab() {
    rowsBody = h('div', { class: 'crows' });
    drawRows();
    return rowsBody;
  }

  function drawRows() {
    if (!rowsBody || !cur) return;
    const c = cur;
    meterEls = new Map();
    if (!c.rows) { rowsBody.replaceChildren(h('p', { class: 'muted' }, 'Loading…')); return; }
    if (!c.rows.length) {
      rowsBody.replaceChildren(empty('No containers yet — the universe is not started.',
        state.readOnly ? null : h('div', { class: 'actions' },
          h('button', { type: 'button', class: 'btn small primary', onclick: () => runSafely({ action: 'up', multiverse: c.multiverse, universe: c.universe }) }, 'Start it'))));
      return;
    }
    rowsBody.replaceChildren(...c.rows.map((r) => {
      const key = r.app + '/' + r.service;
      const numbers = h('div', { class: 'meters' });
      meterEls.set(r.name, numbers);
      return h('div', { class: 'crow' + (r.running ? '' : ' off') },
        h('div', { class: 'crow-head' }, h('span', { class: 'dot ' + (r.running ? 'ok' : 'warn') }), h('b', null, key),
          h('span', { class: 'grow' }), h('span', { class: 'muted small' }, r.status)),
        numbers,
        ...urlsFor(c, r),
        r.ports ? h('div', { class: 'muted mono small' }, r.ports) : null,
        h('div', { class: 'actions' },
          h('button', { type: 'button', class: 'btn small', onclick: () => startLog(r.app, r.service) }, 'Logs'),
          restartButton(c, r, key),
          copyable('multiversectl universe exec ' + c.universe + ' ' + r.app + ' --service ' + r.service + ' -- sh', 'shell command'),
          h('span', { class: 'grow' }),
          copyable(r.name, 'name')));
    }));
    drawStats();
  }

  // Where this container is reached: the addresses the proxy routes to this
  // very service, which is what somebody looking at a container wants to open.
  // Only a web address becomes a link, as everywhere else on this page.
  function urlsFor(c, r) {
    const uni = universeIn(c.multiverse, c.universe);
    const app = uni && (uni.apps || []).find((a) => a.key === r.app);
    if (!app) return [];
    return (app.urls || []).filter((x) => x.service === r.service && /^https?:\/\//.test(x.url))
      .map((x) => h('div', { class: 'crow-url' },
        h('a', { href: x.url, target: '_blank', rel: 'noopener noreferrer' }, x.url)));
  }

  function restartButton(c, r, key) {
    if (state.readOnly || !r.running) return null;
    if (c.confirm === key) {
      return h('button', {
        type: 'button', class: 'btn small danger',
        onclick: async () => {
          c.confirm = null;
          try { await run({ action: 'restart', multiverse: c.multiverse, universe: c.universe, app: r.app, service: r.service }); }
          catch (e) { toast(e.message); }
          drawRows();
        },
      }, 'Restart, really');
    }
    return h('button', {
      type: 'button', class: 'btn small', disabled: isBusy(c.multiverse),
      title: 'Restart the container in place; it does not pick up a changed image',
      onclick: () => { c.confirm = key; drawRows(); setTimeout(() => { if (cur === c && c.confirm === key) { c.confirm = null; drawRows(); } }, 4000); },
    }, 'Restart');
  }

  function drawStats() {
    if (!cur || !meterEls) return;
    const c = cur;
    for (const [name, into] of meterEls) {
      const st = c.stats[name];
      const row = (c.rows || []).find((r) => r.name === name);
      if (!st) { into.replaceChildren(row && row.running ? h('span', { class: 'muted small' }, 'reading…') : h('span', { class: 'muted small' }, 'not running')); continue; }
      into.replaceChildren(
        meter('CPU', st.cpu, viz.percent(st.cpu)),
        meter('Mem', st.memory, viz.bytes(st.mem || 0), st.memUsage),
        viz.sparkline(c.history.get(name) || [], Date.now() - SPARK_POINTS * STATS_MS, Date.now(), '', 70, 18),
        h('span', { class: 'muted small' }, 'net ' + (st.netIO || '—') + ' · ' + st.pids + ' pids'));
    }
  }

  const meter = (label, pct, text, title) => h('span', { class: 'meter', title: title || label + ' ' + text },
    h('span', { class: 'meter-l' }, label),
    h('span', { class: 'bar-track' }, h('span', { class: 'bar-fill w' + Math.min(10, Math.max(0, Math.round(pct / 10))) })),
    h('span', { class: 'mono small' }, text));

  const LOG_STATE = { loading: ['mute', 'opening…'], loaded: ['ok', 'last lines'], following: ['ok', 'following'], ended: ['mute', 'ended'], failed: ['bad', 'stopped'] };

  function logsTab() {
    const c = cur;
    const log = c.log;
    if (!log) {
      const choices = (c.rows || []);
      if (!choices.length) return h('p', { class: 'muted' }, 'Start the universe to read its logs.');
      return h('div', null, h('p', { class: 'muted' }, 'Which service?'),
        h('div', { class: 'actions wrap' }, choices.map((r) => h('button', {
          type: 'button', class: 'btn small', onclick: () => startLog(r.app, r.service),
        }, h('span', { class: 'dot ' + (r.running ? 'ok' : 'warn') }), r.app + '/' + r.service))));
    }
    logState = h('span', { class: 'log-state' });
    logCount = h('span', { class: 'muted small' });
    logOut = h('div', { class: 'logout mono' + (log.wrap ? ' wrap' : ''), tabindex: '0', role: 'log', 'aria-label': 'Log of ' + log.appKey + '/' + log.service });
    log.drawn = 0;
    logOut.addEventListener('scroll', () => { if (jump) jump.hidden = atEnd(); });

    const services = h('select', {
      'aria-label': 'Service', onchange: (e) => startLog(...e.target.value.split('/')),
    }, (c.rows || []).map((r) => h('option', { value: r.app + '/' + r.service, selected: r.app === log.appKey && r.service === log.service }, r.app + '/' + r.service)));
    const tailSel = h('select', { 'aria-label': 'Lines of history', onchange: (e) => { log.tail = Number(e.target.value); startLog(log.appKey, log.service); } },
      [100, 200, 500, 1000, 2000].map((n) => h('option', { value: n, selected: n === log.tail }, 'last ' + n)));
    const filter = h('input', { type: 'search', placeholder: 'Only lines containing…', value: log.filter, 'aria-label': 'Filter the lines', oninput: (e) => { log.filter = e.target.value; log.drawn = 0; drawLog(); } });
    const toggle = (name, on, set) => h('label', { class: 'switch' }, h('input', { type: 'checkbox', checked: on, onchange: (e) => set(e.target.checked) }), name);
    jump = h('button', { type: 'button', class: 'btn small jump', hidden: true, onclick: () => { logOut.scrollTop = logOut.scrollHeight; jump.hidden = true; } }, '↓ latest');

    const panel = h('div', { class: 'logs' },
      h('div', { class: 'bar' }, services, logState, h('span', { class: 'grow' }), tailSel,
        toggle('follow', log.follow, (on) => { log.follow = on; startLog(log.appKey, log.service); })),
      h('div', { class: 'bar' }, filter, logCount, h('span', { class: 'grow' }),
        toggle('wrap', log.wrap, (on) => { log.wrap = on; logOut.classList.toggle('wrap', on); }),
        toggle('times', log.times, (on) => { log.times = on; log.drawn = 0; drawLog(); })),
      h('div', { class: 'log-wrap' }, logOut, jump),
      h('div', { class: 'bar' },
        h('button', { type: 'button', class: 'btn small', onclick: () => { log.lines = []; log.drawn = 0; drawLog(); } }, 'Clear'),
        h('button', {
          type: 'button', class: 'btn small',
          onclick: async () => {
            const n = visible(log).length;
            toast(await wrote(asText(log)) ? 'Copied ' + plural(n, 'line', 'lines') : 'Copy failed; select the lines and press ctrl-C');
          },
        }, 'Copy'),
        h('button', { type: 'button', class: 'btn small', onclick: () => download(log) }, 'Download'),
        h('span', { class: 'grow' }),
        h('span', { class: 'muted small' }, 'docker logs of ', log.appKey, '/', log.service)));
    drawLogState();
    queueMicrotask(drawLog);
    return panel;
  }

  function download(log) {
    const name = cur.multiverse + '-' + cur.universe + '-' + log.appKey + '-' + log.service + '.log';
    const url = URL.createObjectURL(new Blob([asText(log)], { type: 'text/plain' }));
    const a = h('a', { href: url, download: name });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
  }

  const atEnd = () => !logOut || logOut.scrollHeight - logOut.scrollTop - logOut.clientHeight < 40;

  // Only what is new is added: a log that is followed must not rebuild three
  // thousand lines every time one arrives.
  function drawLog() {
    if (!cur || !cur.log || !logOut) return;
    const log = cur.log;
    const stick = atEnd();
    if (log.drawn <= 0) { logOut.replaceChildren(); log.drawn = 0; log.shed = 0; }
    const note = logOut.querySelector('.log-note');
    if (note) note.remove();
    for (let i = log.shed || 0; i > 0 && logOut.firstElementChild; i--) logOut.firstElementChild.remove();
    log.shed = 0;
    const add = [];
    for (let i = log.drawn; i < log.lines.length; i++) {
      const l = log.lines[i];
      if (shows(log, l)) add.push(lineEl(log, l));
    }
    log.drawn = log.lines.length;
    if (add.length) logOut.append(...add);
    const shown = logOut.childElementCount;
    logCount.textContent = log.filter ? shown + ' of ' + log.lines.length + ' lines' : plural(shown, 'line', 'lines');
    if (!shown) logOut.append(h('p', { class: 'muted log-note' }, log.filter ? 'No line contains that.' : 'Nothing logged yet.'));
    if (stick) logOut.scrollTop = logOut.scrollHeight;
    if (jump) jump.hidden = atEnd();
  }

  function drawLogState() {
    if (!cur || !cur.log || !logState) return;
    const log = cur.log;
    const [kind, text] = LOG_STATE[log.state] || ['mute', log.state];
    logState.replaceChildren(h('span', { class: 'dot ' + (kind === 'bad' ? 'warn' : kind === 'ok' ? 'ok' : '') }),
      h('span', { class: 'muted small' }, text + (log.detail ? ': ' + log.detail : '')));
  }

  // Escape closes the panel, but a dialog on top of it gets it first.
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && cur && !document.querySelector('dialog[open]')) close();
  });
  return { open, close, reload };
})();
