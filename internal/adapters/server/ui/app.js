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

async function api(path) {
  const res = await fetch('/api/' + path, { headers: { Accept: 'application/json' } });
  let body = {};
  try { body = await res.json(); } catch (_) { /* leave empty */ }
  if (!res.ok) throw new Error(body.error || res.statusText);
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
    return {
      async refresh(fetch) {
        try {
          const { version, status } = await api('overview' + (fetch ? '?fetch=1' : ''));
          document.getElementById('version').textContent = version;
          body.replaceChildren(...overview(status));
        } catch (e) { body.replaceChildren(errorBox(e)); }
      },
    };
  },
};

function overview(st) {
  const out = [];
  if (st.next) out.push(h('div', { class: 'next' }, h('b', null, 'Next'), copyable(st.next)));

  const root = st.root || {};
  const rootCard = h('div', { class: 'card' }, h('h3', null, 'Root catalog'));
  if (!root.source) {
    rootCard.append(h('p', { class: 'muted' }, 'None. Projects can still be installed from a repository.'),
      h('p', null, cmd('multiversectl init')));
  } else {
    rootCard.append(h('div', { class: 'mono', style: 'word-break:break-all' }, root.source));
    if (root.unavailable) rootCard.append(h('p', null, badge('bad', 'unavailable'), ' ', root.unavailable));
    if (root.noRemote) rootCard.append(h('p', null, badge('warn', 'local only'), ' Lost with this machine, and nobody else can use it.'));
    rootCard.append(h('p', null, h('a', { href: '#/catalog' }, 'Browse the tree →')));
  }
  const ws = st.workspaces || [];
  const stale = ws.filter((w) => w.stale).length;
  out.push(h('div', { class: 'cards' },
    rootCard,
    h('div', { class: 'card' }, h('h3', null, 'Installed'), h('div', { class: 'big' }, ws.length),
      h('div', { class: 'muted' }, ws.length === 1 ? 'workspace' : 'workspaces')),
    h('div', { class: 'card' }, h('h3', null, 'Drift'), h('div', { class: 'big' }, stale),
      h('div', { class: 'muted' }, stale ? 'need an update' : 'all up to date'))));

  out.push(h('h2', null, 'Workspaces'));
  if (!ws.length) {
    out.push(empty('Nothing installed yet.', h('br'), cmd('multiversectl catalog install <owner/repo>')));
    return out;
  }
  out.push(h('div', { class: 'scroll' }, h('table', null,
    h('thead', null, h('tr', null, ['Workspace', 'From', 'State', 'Root'].map((t) => h('th', null, t)))),
    h('tbody', null, ws.map((w) => h('tr', null,
      h('td', null, w.current ? h('span', { title: 'current workspace' }, '★ ') : '',
        h('a', { href: '#/workspace/' + encodeURIComponent(w.name) }, w.name)),
      h('td', { class: 'mono' }, w.from || w.source || '—'),
      h('td', null, w.notes && w.notes.length
        ? w.notes.map((n) => [badge(w.stale ? 'warn' : 'mute', n), ' '])
        : badge('ok', 'up to date')),
      h('td', null, copyable(w.root))))))));
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

    const matches = (n) => !filter || [n.name, n.path, n.repo || ''].some((s) => s.toLowerCase().includes(filter));
    const keeps = (n) => matches(n) || (n.children || []).some(keeps);

    function item(n) {
      if (!keeps(n)) return null;
      const label = [
        h('span', { class: 'name' }, n.path === '' ? n.name + ' (root)' : n.name),
        n.unavailable ? badge('bad', '✗ ' + n.unavailable) : null,
        n.installedAs ? badge('ok', '✓ installed as ' + n.installedAs) : null,
        n.repo ? h('span', { class: 'repo' }, n.repo + (n.ref ? '@' + n.ref : '')) : null,
      ];
      if (n.kind === 'workspace') {
        return h('li', { class: 'leaf' }, h('div', { class: 'node ws' }, label,
          n.installedAs ? null : copyable('multiversectl catalog install ' + n.path, 'install')));
      }
      const kids = (n.children || []).map(item).filter(Boolean);
      const d = h('details', { ontoggle: (e) => { e.target.open ? open.add(n.path) : open.delete(n.path); } },
        h('summary', null, h('div', { class: 'node cat' }, label)),
        kids.length ? h('ul', null, kids) : h('p', { class: 'muted', style: 'margin:2px 0 2px 44px' }, n.unavailable ? '' : 'empty'));
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
        // Open the tree the first time, so the workspaces are in view; a large
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
    u.checkouts.map((c) => h('span', null, [c.repo + (c.branch ? ' @ ' + c.branch : '') + ' ', copyable(c.path)])));
  const panel = h('div');
  return h('article', { class: 'card uni' },
    h('header', null, h('span', { class: 'dot ' + (u.running ? 'ok' : '') }), h('h3', null, u.name),
      badge(u.running ? 'ok' : 'mute', u.running ? u.running + (total ? '/' + total : '') + ' running' : 'stopped'),
      h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, u.name, e.target) }, 'Doctor')),
    h('div', { class: 'meta' }, [u.created && 'created ' + ago(u.created), u.lastStarted && 'started ' + ago(u.lastStarted), u.domain].filter(Boolean).join(' · ')),
    u.note ? h('p', { class: 'note' }, u.note) : null,
    apps, dir, panel);
}

async function runDoctor(into, workspace, universe, btn) {
  btn.disabled = true;
  into.replaceChildren(h('p', { class: 'muted' }, 'Checking…'));
  try {
    const q = 'doctor?workspace=' + encodeURIComponent(workspace) + (universe ? '&universe=' + encodeURIComponent(universe) : '');
    const rep = await api(q);
    const kind = { ok: 'ok', warn: 'warn', fail: 'bad' };
    into.replaceChildren(h('div', { class: 'panel' },
      h('b', null, rep.ok ? 'No failures' : 'Something failed'),
      h('ul', { class: 'checks' }, rep.checks.map((c) => h('li', null,
        badge(kind[c.status] || 'mute', c.status), h('span', { class: 'mono' }, c.name), h('span', null, c.detail || ''))))));
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
      h('p', { class: 'lede' }, 'Every universe of every installed workspace, and what is running in it.'),
      h('div', { class: 'bar' }, search), body);
    const wanted = (u) => !filter || [u.name, u.note, ...Object.values(u.branches || {})].some((s) => (s || '').toLowerCase().includes(filter));

    function draw() {
      if (!data) return;
      if (!data.workspaces.length) {
        body.replaceChildren(empty('Nothing installed yet.', h('br'), cmd('multiversectl catalog install <owner/repo>')));
        return;
      }
      body.replaceChildren(...data.workspaces.map((w) => {
        const list = w.universes.filter(wanted);
        const panel = h('div');
        return h('section', null,
          h('div', { class: 'ws-head' }, h('h2', null, h('a', { href: '#/workspace/' + encodeURIComponent(w.name) }, w.name)),
            w.current ? badge('info', 'current') : null,
            h('span', { class: 'muted' }, w.universes.length + (w.universes.length === 1 ? ' universe' : ' universes')),
            h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, '', e.target) }, 'Doctor')),
          panel,
          w.error ? errorBox(w.error) : null,
          (w.warnings || []).map((x) => h('p', { class: 'muted' }, badge('warn', 'warning'), ' ', x)),
          list.length ? h('div', { class: 'universes' }, list.map((u) => universeCard(w, u)))
            : (!w.error && h('div', { class: 'empty' }, w.universes.length ? 'No universe matches.' : ['No universes yet. ', cmd('multiversectl -w ' + w.name + ' universe new 1')])));
      }));
    }
    return {
      async refresh() {
        try { data = await api('fleet'); draw(); }
        catch (e) { body.replaceChildren(errorBox(e)); }
      },
      focus: () => search.focus(),
    };
  },
};

views.workspace = {
  title: 'Workspace',
  mount(host, name) {
    const body = h('div');
    host.append(h('p', null, h('a', { href: '#/' }, '← Overview')), h('h1', null, name), body);
    const panel = h('div');
    return {
      async refresh() {
        try {
          const w = await api('workspaces/' + encodeURIComponent(name));
          body.replaceChildren(
            h('p', { class: 'lede' }, w.current ? badge('info', 'current') : null, ' ', copyable(w.root)),
            h('div', { class: 'cards' },
              h('div', { class: 'card' }, h('h3', null, 'Reach'), h('div', null, 'domain ', h('b', null, w.domain), ' · ', w.expose || 'default')),
              h('div', { class: 'card' }, h('h3', null, 'Proxy'), h('div', { class: 'mono' }, w.network),
                h('div', null, Object.entries(w.entrypoints || {}).map(([k, v]) => [h('span', { class: 'chip' }, k + ' :' + v), ' '])))),
            h('h2', null, 'Repositories'),
            h('div', { class: 'scroll' }, h('table', null,
              h('thead', null, h('tr', null, ['Name', 'URL', 'Runtime', 'Clone'].map((t) => h('th', null, t)))),
              h('tbody', null, w.repos.map((r) => h('tr', null, h('td', null, r.key), h('td', { class: 'mono' }, r.url),
                h('td', null, r.runtime || 'compose'), h('td', null, r.cloned ? badge('ok', 'cloned') : badge('warn', 'not cloned'))))))),
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

// ── shell ──────────────────────────────────────────────────────────────

let current = null, timer = null;

function route() {
  const [, page = '', arg] = location.hash.split('/');
  const key = page === '' ? 'overview' : page;
  const view = views[key] || views.overview;
  document.querySelectorAll('.rail a').forEach((a) => {
    if (a.dataset.route === (key === 'workspace' ? 'overview' : key)) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });
  main.replaceChildren();
  current = view.mount(main, arg ? decodeURIComponent(arg) : undefined);
  document.title = view.title + ' · multiversectl';
  refresh();
  main.focus({ preventScroll: true });
}

async function refresh(fetch) {
  if (!current) return;
  await current.refresh(fetch);
  document.getElementById('stamp').textContent = 'updated ' + new Date().toLocaleTimeString();
}

function schedule() {
  clearInterval(timer);
  if (!document.getElementById('auto').checked) return;
  // Only the views whose data moves; the catalog changes when someone commits.
  timer = setInterval(() => {
    if (!document.hidden && /^#\/(fleet|workspace\/.*)?$|^$/.test(location.hash)) refresh();
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
document.getElementById('refresh').addEventListener('click', (e) => { e.target.disabled = true; refresh().finally(() => { e.target.disabled = false; }); });
document.getElementById('auto').addEventListener('change', schedule);
document.addEventListener('keydown', (e) => {
  if (e.key === '/' && !/input|textarea/i.test(document.activeElement.tagName) && current && current.focus) { e.preventDefault(); current.focus(); }
});
window.addEventListener('hashchange', route);
route();
schedule();
