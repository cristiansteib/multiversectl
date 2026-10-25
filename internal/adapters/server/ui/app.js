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
    const draw = (status) => { last = status; body.replaceChildren(...overview(status)); };
    return {
      async refresh(fetch) {
        try {
          const { readOnly, status } = await api('overview' + (fetch ? '?fetch=1' : ''));
          if (readOnly) state.readOnly = true;
          draw(status);
        } catch (e) { body.replaceChildren(errorBox(e)); }
      },
      onOverview(status) { draw(status); },
      onOps() { if (last) draw(last); },
      onMode() { if (last) draw(last); },
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
    rootCard.append(h('div', { class: 'mono wrap' }, root.source));
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
    h('thead', null, h('tr', null, ['Workspace', 'From', 'State', 'Root', ''].map((t) => h('th', null, t)))),
    h('tbody', null, ws.map((w) => h('tr', null,
      h('td', null, w.current ? h('span', { title: 'current workspace' }, '★ ') : '',
        h('a', { href: '#/workspace/' + encodeURIComponent(w.name) }, w.name)),
      h('td', { class: 'mono' }, w.from || w.source || '—'),
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
  const busy = isBusy(w.name);
  const act = (action, label, kind) => h('button', { type: 'button', class: 'btn small ' + (kind || ''), disabled: busy,
    title: busy ? 'Another operation is running in ' + w.name : null,
    onclick: () => run({ action, workspace: w.name, universe: u.name }) }, label);
  const actions = state.readOnly ? null : h('div', { class: 'actions' },
    u.running < total || !u.running ? act('up', 'Start', 'primary') : null,
    u.running ? act('down', 'Stop') : null,
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
    u.checkouts.map((c) => h('span', null, [c.repo + (c.branch ? ' @ ' + c.branch : '') + ' ', copyable(c.path)])));
  const panel = h('div');
  const saved = doctorResults.get(w.name + '/' + u.name);
  if (saved) panel.append(saved);
  return h('article', { class: 'card uni' },
    h('header', null, h('span', { class: 'dot ' + (u.running ? 'ok' : '') }), h('h3', null, u.name),
      badge(u.running ? 'ok' : 'mute', u.running ? u.running + (total ? '/' + total : '') + ' running' : 'stopped'),
      h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'btn small', onclick: (e) => runDoctor(panel, w.name, u.name, e.target) }, 'Doctor')),
    actions,
    h('div', { class: 'meta' }, [u.created && 'created ' + ago(u.created), u.lastStarted && 'started ' + ago(u.lastStarted), u.domain].filter(Boolean).join(' · ')),
    u.note ? h('p', { class: 'note' }, u.note) : null,
    apps, dir, panel);
}

// What a doctor run said, kept by what it ran on, because the fleet is redrawn
// whenever something changes and a result should not vanish with it.
const doctorResults = new Map();

async function runDoctor(into, workspace, universe, btn) {
  btn.disabled = true;
  const key = workspace + '/' + universe;
  into.replaceChildren(h('p', { class: 'muted' }, 'Checking…'));
  try {
    const q = 'doctor?workspace=' + encodeURIComponent(workspace) + (universe ? '&universe=' + encodeURIComponent(universe) : '');
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
      h('p', { class: 'lede' }, 'Every universe of every installed workspace, and what is running in it.'),
      h('div', { class: 'bar' }, search), body);
    const wanted = (u) => !filter || [u.name, u.note, ...Object.values(u.branches || {})].some((s) => (s || '').toLowerCase().includes(filter));

    function draw() {
      if (!data) return;
      if (data.error) { body.replaceChildren(errorBox(data.error)); return; }
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
            h('span', { class: 'grow' }),
            wsActions(w, { create: true, update: true, use: !w.current }, w),
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

views.workspace = {
  title: 'Workspace',
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
          const w = await api('workspaces/' + encodeURIComponent(name));
          body.replaceChildren(
            h('p', { class: 'lede' }, w.current ? badge('info', 'current') : null, ' ', copyable(w.root)),
            state.readOnly ? null : h('div', { class: 'bar' }, wsActions({ name: w.name, current: w.current, universes: [] }, { create: true, update: true, use: !w.current })),
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

// ── actions ────────────────────────────────────────────────────────────
// Everything the page can do is a use case the command line has too. The
// server runs it in the background and sends its output as it happens; the
// dock at the bottom is where that is read.

const isBusy = (workspace) => state.ops.some((o) => o.workspace === workspace && o.state === 'running');

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

// The buttons that act on a whole workspace. A workspace row may not know its
// universes, so the caller can pass the fleet entry that does.
function wsActions(w, want, fleetEntry) {
  if (state.readOnly) return null;
  const busy = isBusy(w.name);
  const tip = busy ? 'Another operation is running in ' + w.name : null;
  return h('span', { class: 'actions' },
    want.create ? h('button', { type: 'button', class: 'btn small primary', disabled: busy, title: tip, onclick: () => newDialog(w, fleetEntry) }, 'New universe') : null,
    want.update ? h('button', { type: 'button', class: 'btn small', disabled: busy, title: tip || 'Fetch the manifest and clone what it newly lists', onclick: () => runSafely({ action: 'update', workspace: w.name }) }, 'Update') : null,
    want.use ? h('button', { type: 'button', class: 'btn small', disabled: busy, title: tip || 'Make it the workspace commands act on by default', onclick: () => runSafely({ action: 'use', workspace: w.name }) }, 'Make current') : null);
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
        await run({ action: 'rm', workspace: w.name, universe: u.name, confirm: input.value, force });
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

function newDialog(w, fleetEntry) {
  const used = new Set(((fleetEntry && fleetEntry.universes) || (state.fleet && (state.fleet.workspaces.find((x) => x.name === w.name) || {}).universes) || []).map((u) => u.name));
  let next = 1;
  while (used.has(String(next))) next++;
  const name = h('input', { type: 'text', name: 'name', value: String(next), autocomplete: 'off', oninput: () => ctl.refresh(), 'aria-label': 'Number or name' });
  const note = h('input', { type: 'text', name: 'note', autocomplete: 'off', placeholder: 'why it exists, shown beside it', 'aria-label': 'Note' });
  const branch = h('input', { type: 'text', name: 'branch', autocomplete: 'off', placeholder: 'default: u' + next, 'aria-label': 'Branch' });
  const ctl = modal({
    title: 'New universe in ' + w.name, confirm: 'Create', ready: () => name.value.trim() !== '',
    body: [
      h('label', { class: 'field' }, h('span', null, 'Number or name'), name,
        h('small', { class: 'muted' }, 'A name such as fix/login is also the branch it checks out.')),
      h('label', { class: 'field' }, h('span', null, 'Branch in every repository (optional)'), branch),
      h('label', { class: 'field' }, h('span', null, 'Note (optional)'), note),
    ],
    async submit() { await run({ action: 'new', workspace: w.name, name: name.value.trim(), branch: branch.value.trim(), note: note.value.trim() }); },
  });
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
    case 'overview': state.overview = e.data; if (current && current.onOverview) current.onOverview(e.data.status); stamp(); break;
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
    if (a.dataset.route === (key === 'workspace' ? 'overview' : key)) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });
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
