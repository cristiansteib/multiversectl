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
const REFRESH_MS = 10000;

// What the page knows, fed by the websocket. A view reads from it and is told
// when a piece changes; when the socket is down the views fetch for themselves.
const state = { connected: false, readOnly: false, hello: null, fleet: null, overview: null, ops: [], shown: null, dockOpen: false };

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

// A value people want in a terminal: one click copies it.
function copyable(text, label) {
  return h('button', {
    type: 'button', class: 'copy mono', title: 'Copy',
    onclick: async () => {
      try { await navigator.clipboard.writeText(text); toast('Copied: ' + text); }
      catch (_) { toast('Copy failed; select it by hand'); }
    },
  }, label || text);
}

const badge = (kind, text, title) => h('span', { class: 'badge ' + kind, title }, text);
const empty = (...kids) => h('div', { class: 'empty' }, kids);
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
  project: { d: 'M8 1.5l5.5 3v7L8 14.5l-5.5-3v-7zM2.5 4.5L8 7.5l5.5-3M8 7.5v7', stroke: true },
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

  const root = st.root || {};
  const rootCard = h('div', { class: 'card' }, h('h3', null, 'Root catalog'));
  if (!root.source) {
    rootCard.append(h('p', { class: 'muted' }, 'None. Projects can still be installed from a repository.'),
      h('p', null, cmd('multiversectl init')));
  } else {
    rootCard.append(h('div', null, place(rootLoc) || h('span', { class: 'mono wrap' }, root.source), ' ',
      root.unavailable ? null : root.legacy ? badge('warn', 'plain Catalog', 'Not marked as a root: run multiversectl init') : badge('ok', 'CatalogRoot')));
    if (root.unavailable) rootCard.append(h('p', null, badge('bad', 'unavailable'), ' ', root.unavailable));
    if (root.noRemote) rootCard.append(h('p', null, badge('warn', 'local only'), ' Lost with this machine, and nobody else can use it.'));
    rootCard.append(h('p', null, h('a', { href: '#/catalog' }, 'Browse the tree →')));
  }
  const ws = st.projects || [];
  const stale = ws.filter((w) => w.stale).length;
  out.push(h('div', { class: 'cards' },
    rootCard,
    h('div', { class: 'card' }, h('h3', null, 'Installed'), h('div', { class: 'big' }, ws.length),
      h('div', { class: 'muted' }, ws.length === 1 ? 'project' : 'projects')),
    h('div', { class: 'card' }, h('h3', null, 'Drift'), h('div', { class: 'big' }, stale),
      h('div', { class: 'muted' }, stale ? 'need an update' : 'all up to date'))));

  out.push(h('h2', null, 'Projects'));
  if (!ws.length) {
    out.push(empty('Nothing installed yet.', h('br'), cmd('multiversectl project install <owner/repo>')));
    return out;
  }
  out.push(h('div', { class: 'scroll' }, h('table', null,
    h('thead', null, h('tr', null, ['Project', 'Catalog path', 'Source', 'State', 'Root', ''].map((t) => h('th', null, t)))),
    h('tbody', null, ws.map((w) => h('tr', null,
      h('td', null, w.current ? h('span', { title: 'current project' }, '★ ') : '',
        h('a', { href: '#/project/' + encodeURIComponent(w.name) }, w.name)),
      h('td', { class: 'mono' }, w.from || '—'),
      h('td', null, place(sources[w.name]) || h('span', { class: 'muted' }, '—')),
      h('td', null, w.notes && w.notes.length
        ? w.notes.map((n) => [badge(w.stale ? 'warn' : 'mute', n), ' '])
        : badge('ok', 'up to date')),
      h('td', null, copyable(w.root)),
      h('td', { class: 'row-actions' }, wsActions(w, { update: true, use: !w.current }))))))));
  return out;
}

views.catalog = {
  title: 'Catalog',
  mount(host) {
    const open = new Set(['']);
    let filter = '', data = null, first = true;
    const body = h('div');
    const search = h('input', {
      type: 'search', name: 'filter', placeholder: 'Filter by name, path or repository   ( / )', 'aria-label': 'Filter the catalog',
      oninput: (e) => { filter = e.target.value.trim().toLowerCase(); draw(); },
    });
    const all = (state) => () => {
      const walk = (n) => { if (n.kind === 'catalog') { state ? open.add(n.path) : open.delete(n.path); (n.children || []).forEach(walk); } };
      if (data) { walk(data.root); open.add(''); draw(); }
    };
    host.append(h('h1', null, 'Catalog'),
      h('p', { class: 'lede' }, 'Your index of projects, as a tree. Nodes are addressed by path, like acme/backend/billing.'),
      h('div', { class: 'bar' }, search,
        h('button', { type: 'button', class: 'btn', onclick: all(true) }, 'Expand all'),
        h('button', { type: 'button', class: 'btn', onclick: all(false) }, 'Collapse all'),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'btn', onclick: (e) => load(true, e.target) }, 'Fetch remotes')),
      body);

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
        n.kind === 'project' && !n.unavailable ? (n.installedAs ? badge('ok', '✓ installed as ' + n.installedAs) : badge('mute', '○ not installed')) : null,
      ];
      if (n.kind === 'project') {
        return h('li', { class: 'leaf' }, h('div', { class: 'node ws' }, label,
          n.installedAs ? null : copyable('multiversectl project install ' + n.path, 'install')));
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
      try {
        data = await api('catalog' + (fetch ? '?fetch=1' : ''));
        // Open the tree the first time, so the projects are in view; a large
        // catalog stays closed, and the filter opens what it finds.
        if (first) { first = false; const count = (n) => 1 + (n.children || []).reduce((a, c) => a + count(c), 0); if (count(data.root) <= 150) all(true)(); }
        draw(); if (fetch) toast('Fetched');
      }
      catch (e) { body.replaceChildren(errorBox(e)); if (/no root catalog/.test(e.message)) body.append(empty(cmd('multiversectl init'))); }
      finally { if (btn) btn.disabled = false; }
    }
    return { refresh: () => load(false), focus: () => search.focus() };
  },
};

const STATE_DOT = { running: 'ok', stopped: 'warn', 'not started': '', optional: '', off: '' };

function universeCard(w, u) {
  const total = u.apps.reduce((n, a) => n + a.expected, 0);
  const busy = isBusy(w.name);
  const act = (action, label, kind) => h('button', { type: 'button', class: 'btn small ' + (kind || ''), disabled: busy,
    title: busy ? 'Another operation is running in ' + w.name : null,
    onclick: () => run({ action, project: w.name, universe: u.name }) }, label);
  const actions = state.readOnly ? null : h('div', { class: 'actions' },
    u.running < total || !u.running ? act('up', 'Start', 'primary') : null,
    u.running ? act('down', 'Stop') : null,
    u.behind ? act('sync', 'Update code') : null,
    h('button', { type: 'button', class: 'btn small danger', disabled: busy, onclick: () => removeDialog(w, u) }, 'Remove…'));
  const apps = u.apps.map((a) => h('div', { class: 'app' },
    h('div', { class: 'app-head' }, h('b', null, a.key),
      a.branch ? h('span', { class: 'chip' }, a.repo + ' @ ' + a.branch) : null,
      a.expected ? badge(a.running === a.expected ? 'ok' : a.running ? 'warn' : 'mute', a.running + '/' + a.expected + ' up') : null),
    a.services && a.services.length ? h('div', { class: 'svcs' }, a.services.map((s) =>
      h('span', { class: 'svc', title: [s.state, s.detail, s.published && 'publishes ' + s.published].filter(Boolean).join(' · ') },
        h('span', { class: 'dot ' + (STATE_DOT[s.state] || '') }), s.name, s.published ? h('span', { class: 'badge warn' }, s.published) : null))) : null,
    a.urls && a.urls.length ? h('div', { class: 'urls' }, a.urls.map((x) =>
      h('a', { href: x.url, target: '_blank', rel: 'noopener noreferrer', title: x.service }, x.url))) : null));
  const dir = h('div', { class: 'paths' }, h('span', null, 'universe'), copyable(u.dir),
    u.checkouts.map((c) => h('span', null, [c.repo + (c.branch ? ' @ ' + c.branch : '') + ' ', copyable(c.path),
      c.behind ? badge('warn', c.behind + ' behind ' + c.follows) : null, c.dirty ? badge('mute', 'edited') : null])));
  const panel = h('div');
  const saved = doctorResults.get(w.name + '/' + u.name);
  if (saved) panel.append(saved);
  return h('article', { class: 'card uni' },
    h('header', null, h('span', { class: 'dot ' + (u.running ? 'ok' : '') }), h('h3', null, u.name),
      badge(u.running ? 'ok' : 'mute', u.running ? u.running + (total ? '/' + total : '') + ' running' : 'stopped'),
      u.behind ? badge('warn', u.behind + ' behind', 'Its code lacks commits of the branches it follows (as of the last fetch). “Update code” brings them in; restart to run them.') : null,
      h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'btn small', title: 'Containers, stats and logs', onclick: () => sidePanel.open(w.name, u.name) }, 'Details'),
      h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, u.name, e.target) }, 'Doctor')),
    actions,
    h('div', { class: 'meta' }, [u.created && 'created ' + ago(u.created), u.lastStarted && 'started ' + ago(u.lastStarted), u.domain].filter(Boolean).join(' · ')),
    // Only a web address becomes a link: the field is free text that anyone with a shell wrote.
    u.pr ? h('p', { class: 'note' }, /^https?:\/\//.test(u.pr) ? h('a', { href: u.pr, target: '_blank', rel: 'noopener noreferrer' }, u.pr) : u.pr) : null,
    u.note ? h('p', { class: 'note' }, u.note) : null,
    apps, dir, panel);
}

// What a doctor run said, kept by what it ran on, because the fleet is redrawn
// whenever something changes and a result should not vanish with it.
const doctorResults = new Map();

async function runDoctor(into, project, universe, btn) {
  btn.disabled = true;
  const key = project + '/' + universe;
  into.replaceChildren(h('p', { class: 'muted' }, 'Checking…'));
  try {
    const q = 'doctor?project=' + encodeURIComponent(project) + (universe ? '&universe=' + encodeURIComponent(universe) : '');
    const rep = await api(q);
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
      h('p', { class: 'lede' }, 'Every universe of every installed project, and what is running in it.'),
      h('div', { class: 'bar' }, search), body);
    const wanted = (u) => !filter || [u.name, u.note, ...Object.values(u.branches || {})].some((s) => (s || '').toLowerCase().includes(filter));

    function draw() {
      if (!data) return;
      if (data.error) { body.replaceChildren(errorBox(data.error)); return; }
      if (!data.projects.length) {
        body.replaceChildren(empty('Nothing installed yet.', h('br'), cmd('multiversectl project install <owner/repo>')));
        return;
      }
      body.replaceChildren(...data.projects.map((w) => {
        const list = w.universes.filter(wanted);
        const panel = h('div');
        return h('section', null,
          h('div', { class: 'ws-head' }, h('h2', null, h('a', { href: '#/project/' + encodeURIComponent(w.name) }, w.name)),
            w.current ? badge('info', 'current') : null,
            h('span', { class: 'muted' }, w.universes.length + (w.universes.length === 1 ? ' universe' : ' universes')),
            h('span', { class: 'grow' }),
            wsActions(w, { create: true, update: true, use: !w.current }, w),
            h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, '', e.target) }, 'Doctor')),
          panel,
          w.error ? errorBox(w.error) : null,
          (w.warnings || []).map((x) => h('p', { class: 'muted' }, badge('warn', 'warning'), ' ', x)),
          list.length ? h('div', { class: 'universes' }, list.map((u) => universeCard(w, u)))
            : (!w.error && h('div', { class: 'empty' }, w.universes.length ? 'No universe matches.' : ['No universes yet. ', cmd('multiversectl -p ' + w.name + ' universe new 1')])));
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

views.project = {
  title: 'Project',
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
          const w = await api('projects/' + encodeURIComponent(name));
          body.replaceChildren(
            h('p', { class: 'lede' }, w.current ? badge('info', 'current') : null, ' ', copyable(w.root)),
            state.readOnly ? null : h('div', { class: 'bar' }, wsActions({ name: w.name, current: w.current, universes: [] }, { create: true, update: true, use: !w.current })),
            h('div', { class: 'cards' },
              h('div', { class: 'card' }, h('h3', null, 'Reach'), h('div', null, 'domain ', h('b', null, w.domain), ' · ', w.expose || 'default')),
              h('div', { class: 'card' }, h('h3', null, 'Proxy'), h('div', { class: 'mono' }, w.network),
                h('div', null, Object.entries(w.entrypoints || {}).map(([k, v]) => [h('span', { class: 'chip' }, k + ' :' + v), ' '])))),
            h('h2', null, 'Repositories'),
            h('div', { class: 'scroll' }, h('table', null,
              h('thead', null, h('tr', null, ['Name', 'Repository', 'Runtime', 'Clone'].map((t) => h('th', null, t)))),
              h('tbody', null, w.repos.map((r) => h('tr', null, h('td', null, r.key), h('td', null, place(r.loc) || h('span', { class: 'mono' }, r.url)),
                h('td', null, r.runtime || 'compose'), h('td', null, r.cloned ? badge('ok', 'cloned') : badge('warn', 'not cloned'), r.behind ? badge('warn', r.behind + ' behind ' + r.follows, 'The shared clone lacks these commits; “Update” on the project brings them in.') : null)))))),
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

const isBusy = (project) => state.ops.some((o) => o.project === project && o.state === 'running');

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

// The buttons that act on a whole project. A project row may not know its
// universes, so the caller can pass the fleet entry that does.
function wsActions(w, want, fleetEntry) {
  if (state.readOnly) return null;
  const busy = isBusy(w.name);
  const tip = busy ? 'Another operation is running in ' + w.name : null;
  return h('span', { class: 'actions' },
    want.create ? h('button', { type: 'button', class: 'btn small primary', disabled: busy, title: tip, onclick: () => newDialog(w) }, 'New universe') : null,
    want.update ? h('button', { type: 'button', class: 'btn small', disabled: busy, title: tip || 'Fetch the manifest and clone what it newly lists', onclick: () => runSafely({ action: 'update', project: w.name }) }, 'Update') : null,
    want.use ? h('button', { type: 'button', class: 'btn small', disabled: busy, title: tip || 'Make it the project commands act on by default', onclick: () => runSafely({ action: 'use', project: w.name }) }, 'Make current') : null);
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
        await run({ action: 'rm', project: w.name, universe: u.name, confirm: input.value, force });
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
        await run({ action: 'new', project: w.name, name: name.value.trim(), base: base.value.trim(), branch: newBranch.value.trim(), note: note.value.trim() });
      } else {
        await run({ action: 'checkout', project: w.name, branch: pick.value.trim(), name: pickName.value.trim(), onlyExisting: only.checked, note: note.value.trim() });
      }
    },
  });

  // What the server knows that a form cannot: the number that is free (not just
  // one no universe has) and the branches there are.
  api('next?project=' + encodeURIComponent(w.name)).then((n) => {
    if (!name.value) name.value = String(n.number);
    if (n.skipped && n.skipped.length) skipped.textContent = 'Skipped ' + n.skipped.join('; ') + '.';
    ctl.refresh();
  }).catch(() => {});
  api('branches?project=' + encodeURIComponent(w.name)).then((r) => {
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
    case 'op': onOp(e.data); break;
    case 'opline': onOpLine(e.data); break;
    default: break;
  }
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
// Every running container of every project, read over and over and drawn as
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

    const projects = () => {
      const seen = new Set();
      for (const s of samples) for (const r of s.rows) seen.add(r.project);
      return [...seen].sort();
    };

    // Every universe seen within the window, so one that has just stopped can
    // still be ticked off rather than disappearing from under the pointer.
    const universesSeen = () => {
      const seen = new Map();
      for (const x of samples) {
        if (x.t < Date.now() - span) continue;
        for (const r of x.rows) seen.set(r.project + '/' + r.universe, r);
      }
      return [...seen.entries()].sort((a, b) => a[0].localeCompare(b[0], undefined, { numeric: true }));
    };

    const shown = (r) => !hidden.has(r.project + '/' + r.universe);
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
      uniLabel.replaceChildren('Universes', h('b', null, all.length && n === all.length ? 'all' : n + ' of ' + all.length));
      // The colour is part of what the menu shows, so it is part of what says
      // the menu has to be drawn again: the palette is claimed by the charts.
      const key = all.map(([k]) => k + ':' + colours.of(k)).join('\u0000') + '|' + [...hidden].sort().join('\u0000');
      if (key === uniShown) return;
      uniShown = key;
      const many = projects().length > 1;
      const set = (k, on) => { if (on) hidden.delete(k); else hidden.add(k); uniShown = ''; draw(); };
      uniMenu.replaceChildren(
        h('div', { class: 'menu-head' },
          h('button', { type: 'button', class: 'btn small ghost', onclick: () => { hidden.clear(); uniShown = ''; draw(); } }, 'All'),
          h('button', { type: 'button', class: 'btn small ghost', onclick: () => { all.forEach(([k]) => hidden.add(k)); uniShown = ''; draw(); } }, 'None')),
        ...all.map(([k, r]) => h('label', { class: 'menu-item' },
          h('input', { type: 'checkbox', checked: !hidden.has(k), onchange: (e) => set(k, e.target.checked) }),
          group === 'universe' ? h('span', { class: 'swatch ' + colours.of(k) }) : null,
          h('span', null, r.universe), many ? h('span', { class: 'muted' }, r.project) : null)));
      if (!all.length) uniMenu.append(h('p', { class: 'muted menu-item' }, 'Nothing is running.'));
    };

    const bar = h('div', { class: 'bar' },
      uniPick,
      pick('Group by', [['universe', 'Universe'], ['app', 'App'], ['service', 'Container']], group, (v) => { group = v; uniShown = ''; draw(); }),
      pick('Window', STATS_WINDOWS, span, (v) => { span = Number(v); draw(); }),
      pick('Read', STATS_EVERY, every, (v) => { every = Number(v); arm(); }),
      pauseBtn, h('span', { class: 'grow' }), note);
    // A menu that stays open over the page is a menu in the way.
    document.addEventListener('click', (e) => { if (uniPick.open && !uniPick.contains(e.target)) uniPick.open = false; });
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
        failed = [{ project: 'this machine', error: e.message }];
        draw();
      } finally { busy = false; }
    }

    function arm() {
      clearInterval(timer);
      timer = paused ? null : setInterval(() => { if (!document.hidden) load(); }, every);
    }

    // ---- grouping
    const keyOf = (r) => group === 'universe' ? r.project + '/' + r.universe
      : group === 'app' ? r.project + '/' + r.universe + '/' + r.app
        : r.project + '/' + r.universe + '/' + r.app + '/' + r.service;
    // With one project its name adds nothing to every label; with two it is
    // what tells one project's universe 1 from the other's.
    const labelOf = (r, many) => {
      const uni = (many ? r.project + ' · ' : '') + r.universe;
      return group === 'universe' ? uni : group === 'app' ? uni + ' ' + r.app : uni + ' ' + r.app + '/' + r.service;
    };


    // series sums each group at each reading. Everything past the eighth group
    // is one line called “other”: a ninth colour nobody could tell apart would
    // be worse than saying “the rest”.
    function build(field) {
      const within = windowed();
      const labels = new Map();
      const order = [];
      const many = projects().length > 1;
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
      errors.replaceChildren(...(failed || []).map((f) => h('p', { class: 'error', role: 'alert' }, f.project + ': ' + f.error)));
      const last = samples.length ? samples[samples.length - 1] : null;
      note.replaceChildren(paused ? badge('warn', 'paused') : badge('ok', 'reading ' + (every / 1000) + 's'),
        last ? ' last at ' + viz.clock(last.t) : ' waiting for the first reading');

      if (!last) { tiles.replaceChildren(); charts.replaceChildren(h('p', { class: 'muted' }, 'Reading…')); facets.replaceChildren(); table.replaceChildren(); return; }
      const rows = last.rows.filter(shown);
      const cpu = rows.reduce((n, r) => n + r.cpu, 0);
      const mem = rows.reduce((n, r) => n + (r.mem || 0), 0);
      const universes = new Set(rows.map((r) => r.project + '/' + r.universe));
      const busiest = rows.slice().sort((a, b) => b.cpu - a.cpu)[0];

      tiles.replaceChildren(
        h('div', { class: 'card' }, h('h3', null, 'Running'), h('div', { class: 'big' }, rows.length),
          h('div', { class: 'muted' }, rows.length === 1 ? 'container' : 'containers', ' in ', universes.size, universes.size === 1 ? ' universe' : ' universes')),
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
          const k = r.project + '/' + r.universe;
          if (!by.has(k)) by.set(k, { project: r.project, universe: r.universe, at: new Map() });
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
        const live = last.rows.some((r) => r.project + '/' + r.universe === k);
        const card = h('article', { class: 'card facet' },
          h('div', { class: 'facet-head' }, h('span', { class: 'dot ' + (live ? 'ok' : '') }), h('b', null, g.universe),
            h('span', { class: 'muted' }, g.project), h('span', { class: 'grow' }), h('b', { class: 'mono' }, viz.percent(current))),
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
          const k = r.project + '/' + r.universe + '/' + r.app + '/' + r.service;
          if (!history.has(k)) history.set(k, []);
          history.get(k).push([s.t, r.cpu]);
        }
      }
      const rows = last.rows.filter(shown).sort((a, b) =>
        a.project.localeCompare(b.project) || a.universe.localeCompare(b.universe, undefined, { numeric: true })
        || a.app.localeCompare(b.app) || a.service.localeCompare(b.service));
      table.replaceChildren(h('h2', null, 'Containers'),
        rows.length ? h('div', { class: 'scroll' }, h('table', { class: 'stats-table' },
          h('thead', null, h('tr', null, ['Universe', 'Service', 'CPU', 'Last ' + (span / 60000) + ' min', 'Memory', 'Network', 'PIDs', ''].map((t) => h('th', null, t)))),
          h('tbody', null, rows.map((r) => {
            const k = r.project + '/' + r.universe + '/' + r.app + '/' + r.service;
            const cls = colours.of(keyOf(r));
            return h('tr', null,
              h('td', null, h('b', null, r.universe), ' ', h('span', { class: 'muted' }, r.project)),
              h('td', null, h('span', { class: 'swatch ' + cls }), r.app + '/' + r.service),
              h('td', { class: 'mono num' }, viz.percent(r.cpu)),
              h('td', null, viz.sparkline(history.get(k) || [], from, Date.now(), cls, 110, 20)),
              h('td', { class: 'mono num', title: r.memUsage }, viz.bytes(r.mem || 0),
                r.memLimit ? h('span', { class: 'muted' }, ' ' + viz.percent(r.memory)) : null),
              h('td', { class: 'mono num muted' }, r.netIO || '—'),
              h('td', { class: 'mono num muted' }, r.pids || 0),
              h('td', { class: 'row-actions' },
                h('button', { type: 'button', class: 'btn small', onclick: () => sidePanel.open(r.project, r.universe, { service: r.app + '/' + r.service }) }, 'Logs')));
          }))))
          : empty('Nothing is running. Start a universe in the ', h('a', { href: '#/fleet' }, 'fleet'), '.'));
    }

    let sized;
    const onResize = () => { clearTimeout(sized); sized = setTimeout(draw, 150); };
    addEventListener('resize', onResize);

    arm();
    return {
      refresh: load,
      stop() { clearInterval(timer); removeEventListener('resize', onResize); },
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
  const [, page = '', arg] = location.hash.split('/');
  const key = page === '' ? 'overview' : page;
  const view = views[key] || views.overview;
  document.querySelectorAll('.rail a').forEach((a) => {
    if (a.dataset.route === (key === 'project' ? 'overview' : key)) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });
  if (current && current.stop) current.stop();
  main.replaceChildren();
  current = view.mount(main, arg ? decodeURIComponent(arg) : undefined);
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
    if (!document.hidden && /^#\/(fleet|project\/.*)?$|^$/.test(location.hash)) refresh();
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

  const query = (c, extra) => new URLSearchParams({ project: c.project, universe: c.universe, ...extra }).toString();

  function close() {
    if (!cur) return;
    stopLog();
    clearInterval(cur.statsTimer);
    cur = null;
    root.hidden = true;
    root.replaceChildren();
    document.body.classList.remove('with-side');
    if (opener && opener.isConnected) opener.focus();
    opener = null;
  }

  // open(project, universe, { service }) — with a service it opens straight on
  // that log, which is what a row elsewhere on the page links to.
  async function open(project, universe, opts) {
    const want = (opts || {}).service;
    if (cur && cur.project === project && cur.universe === universe) {
      if (want) startLog(...want.split('/'));
      return;
    }
    const from = document.activeElement;
    close();
    opener = from;
    cur = { project, universe, rows: null, stats: {}, history: new Map(), tab: 'containers', log: null, statsTimer: null, confirm: null, error: null };
    root.hidden = false;
    document.body.classList.add('with-side');
    draw();
    root.querySelector('.side-head h3').focus();
    await loadRows();
    if (want && cur) startLog(...want.split('/'));
    pollStats();
    cur.statsTimer = setInterval(pollStats, STATS_MS);
  }

  // reload is what an operation elsewhere calls: a service that was just
  // restarted or started should not still read “Exited” here.
  function reload() { if (cur) loadRows(); }

  async function loadRows() {
    const c = cur;
    try { const r = await api('universe/containers?' + query(c)); if (cur === c) { c.rows = r.containers; c.error = null; } }
    catch (e) { if (cur === c) c.error = e.message; }
    if (cur === c) draw();
  }

  async function pollStats() {
    const c = cur;
    if (!c || document.hidden || c.tab !== 'containers' || !(c.rows || []).some((r) => r.running)) return;
    try {
      const r = await api('universe/stats?' + query(c));
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
      const res = await fetch('/api/universe/logs?' + query(c, { app: appKey, service, tail: log.tail, follow: log.follow ? '1' : '0' }), { signal: log.abort.signal });
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
      const text = m ? m[2] : line;
      let level = '';
      const head = text.slice(0, 160);
      for (const [re, name] of LEVEL) if (re.test(head)) { level = name; break; }
      log.lines.push({ at: m ? Date.parse(m[1]) : 0, text, level });
    }
    if (log.lines.length > MAX_LOG_LINES) {
      log.lines.splice(0, log.lines.length - MAX_LOG_LINES);
      log.drawn = 0; // the window moved; the whole view is redrawn
    }
  }

  const shows = (log, l) => !log.filter || l.text.toLowerCase().includes(log.filter.toLowerCase());
  const visible = (log) => log.lines.filter((l) => shows(log, l));
  const asText = (log) => visible(log).map((l) => (log.times && l.at ? new Date(l.at).toISOString() + ' ' : '') + l.text).join('\n');

  // One line, with the part that matched the filter marked in it.
  function lineEl(log, l) {
    const body = h('span', { class: 'lm' });
    const f = log.filter;
    if (f) {
      const low = l.text.toLowerCase(), needle = f.toLowerCase();
      let i = 0;
      for (;;) {
        const at = low.indexOf(needle, i);
        if (at < 0) { body.append(l.text.slice(i)); break; }
        if (at > i) body.append(l.text.slice(i, at));
        body.append(h('mark', null, l.text.slice(at, at + f.length)));
        i = at + f.length;
      }
    } else body.append(l.text);
    return h('div', { class: 'll ' + l.level },
      log.times && l.at ? h('span', { class: 'lt' }, new Date(l.at).toLocaleTimeString(undefined, { hour12: false })) : null,
      body);
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
    const uni = universeIn(c.project, c.universe);
    root.replaceChildren(
      h('header', { class: 'side-head' },
        h('h3', { tabindex: '-1' }, c.universe),
        h('span', { class: 'muted' }, c.project),
        c.rows ? badge(running ? 'ok' : 'mute', running ? running + ' running' : 'stopped') : null,
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'btn small ghost close', 'aria-label': 'Close the panel (Esc)', title: 'Close (Esc)', onclick: close }, '✕')),
      uni && uni.apps ? h('div', { class: 'side-urls' }, uni.apps.flatMap((a) => (a.urls || []).map((x) =>
        h('a', { href: x.url, target: '_blank', rel: 'noopener noreferrer', class: 'chip' }, x.service || a.key)))) : null,
      h('div', { class: 'tabs', role: 'tablist' }, tab('containers', 'Containers', c.rows ? c.rows.length : null), tab('logs', 'Logs')),
      ...(c.error ? [errorBox(c.error)] : []),
      h('div', { id: 'tabpanel', role: 'tabpanel', 'aria-labelledby': 'tab-' + c.tab, class: 'side-body' },
        c.tab === 'containers' ? containersTab() : logsTab()));
  }

  // What the fleet already knows about this universe, when it knows it: the
  // panel does not ask again for what is on the page.
  function universeIn(project, universe) {
    const w = ((state.fleet || {}).projects || []).find((x) => x.name === project);
    return w && (w.universes || []).find((u) => u.name === universe);
  }

  // Each row is built once; only its numbers change as readings come in.
  let meterEls;

  function containersTab() {
    rowsBody = h('div', { class: 'crows' });
    meterEls = new Map();
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
          h('button', { type: 'button', class: 'btn small primary', onclick: () => runSafely({ action: 'up', project: c.project, universe: c.universe }) }, 'Start it'))));
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

  function restartButton(c, r, key) {
    if (state.readOnly || !r.running) return null;
    if (c.confirm === key) {
      return h('button', {
        type: 'button', class: 'btn small danger',
        onclick: async () => {
          c.confirm = null;
          try { await run({ action: 'restart', project: c.project, universe: c.universe, app: r.app, service: r.service }); }
          catch (e) { toast(e.message); }
          drawRows();
        },
      }, 'Restart, really');
    }
    return h('button', {
      type: 'button', class: 'btn small', disabled: isBusy(c.project),
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
        h('button', { type: 'button', class: 'btn small', onclick: async () => { try { await navigator.clipboard.writeText(asText(log)); toast('Copied ' + visible(log).length + ' lines'); } catch (_) { toast('Copy failed'); } } }, 'Copy'),
        h('button', { type: 'button', class: 'btn small', onclick: () => download(log) }, 'Download'),
        h('span', { class: 'grow' }),
        h('span', { class: 'muted small' }, 'docker logs of ', log.appKey, '/', log.service)));
    drawLogState();
    queueMicrotask(drawLog);
    return panel;
  }

  function download(log) {
    const name = cur.project + '-' + cur.universe + '-' + log.appKey + '-' + log.service + '.log';
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
    if (!log.drawn) logOut.replaceChildren();
    const note = logOut.querySelector('.log-note');
    if (note) note.remove();
    const add = [];
    for (let i = log.drawn; i < log.lines.length; i++) {
      const l = log.lines[i];
      if (shows(log, l)) add.push(lineEl(log, l));
    }
    log.drawn = log.lines.length;
    if (add.length) logOut.append(...add);
    const shown = logOut.childElementCount;
    logCount.textContent = log.filter ? shown + ' of ' + log.lines.length + ' lines' : shown + (shown === 1 ? ' line' : ' lines');
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
