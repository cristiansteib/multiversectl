// @ts-nocheck -- the old site's hand-written script for this page,
// carried over as it was. Rewrite it as typed TypeScript when you touch it.
import gsap from 'gsap';
const $ = id => document.getElementById(id);
const per = u => ({
  disk:    [`#u${u}-ui`, `#u${u}-api`, `#u${u}-k8s`],
  written: [`#u${u}-render`, `#u${u}-rui`, `#u${u}-rapi`],
  projects:[`#u${u}-dock`, `#u${u}-pui`, `#u${u}-papi`],
  frame:   `#u${u}-meta`,
  newmark: u === 1 ? [`#u${u}-new`, '#new-trunk'] : `#u${u}-new`,
  upmark:  `#u${u}-up`,
  services:[`#u${u}-web`, `#u${u}-http`, `#u${u}-db`, `#u${u}-redis`],
  vol:     `#u${u}-vol`,
});
const U = {1: per(1), 2: per(2)};
const everything = u => [...U[u].disk, ...U[u].written, ...U[u].projects, U[u].vol, U[u].frame,
  ...[U[u].newmark].flat(), U[u].upmark, `#tok2-u${u}`,
  `#u${u}-voltag`, `#road-u${u}`, `#lbl-u${u}`, `#tok-u${u}`,
  `#u${u}-bar-new`, `#u${u}-bar-up`];

// A card flies in from the thing it came out of: the delta is measured, not
// typed, so moving a box in the markup cannot leave an animation behind.
const mid = el => { const b = el.getBBox(); return {x:b.x + b.width/2, y:b.y + b.height/2}; };
const from = sel => {
  const card = document.querySelector(sel), src = $(card.dataset.from);
  const a = mid(card), b = mid(src);
  return {x:b.x - a.x, y:b.y - a.y, opacity:0, scale:.7, transformOrigin:'50% 50%'};
};
const land = {x:0, y:0, opacity:1, scale:1, duration:.7, ease:'back.out(1.4)', immediateRender:false};
const pop  = {opacity:1, scale:1, duration:.4, ease:'back.out(1.6)', transformOrigin:'50% 50%', immediateRender:false};

// The main boxes are drawn from the first frame — the multiverse, each
// universe, each `in docker`, the proxy and the client. Everything they hold is
// absent until a command makes it, and gone again when one destroys it.
const MADE = [1, 2].flatMap(u => [...U[u].disk, ...U[u].written, ...U[u].projects.slice(1),
  U[u].vol, U[u].frame, ...[U[u].newmark].flat(), U[u].upmark,
  ...U[u].services, `#road-u${u}`, `#lbl-u${u}`, `#tok-u${u}`, `#tok2-u${u}`,
  `#u${u}-voltag`, `#u${u}-bar-new`, `#u${u}-bar-up`]);
gsap.set([...MADE, '#manifest-card', '#manifest-hint',
          '#clone-ui', '#clone-api', '#clone-k8s'], {opacity:0});

const steps = {
  boot: ['start acme/platform --install-only', "One command bootstraps the multiverse, with nothing set up first: it reads the file, clones each repository the file names &mdash; once, for every universe there will ever be &mdash; and stops there. Without <b>--install-only</b> it would go straight on to make universe 1 and start it."],
  new1: ['universe new 1', "The first thing <b>start</b> does: a <b>git worktree</b> of every repository, each on a branch of its own, with the secrets copied in. <b>k8s-manifest</b> gets one too &mdash; it just has nothing to run."],
  up1: ['universe up 1', "Then the config and one routing fragment per app are written, and <b>compose</b> makes a project per app &mdash; <b>acme-u1-ui</b> and <b>acme-u1-api</b>, each on a network of its own. The proxy joins them, so <b>ui-u1.example.test</b> answers, and a <b>curl</b> proves it, and <b>start</b> ends on those addresses."],
  new2: ['universe new fix/cart', "A second universe of the same multiverse, named after its branch rather than numbered, from the same clones. Its hostnames carry <b>fix-cart</b>, and universe 1 keeps running throughout."],
  up2: ['universe up fix/cart', "Universe 2 gets its own projects, its own networks and its own database. Both universes now answer, each on its own hostnames &mdash; so the same <b>curl</b> can be put to both and compared."],
  down2: ['universe down fix/cart', "<b>down</b> on universe 2 alone: its services and both its networks go, and the memory with them. Its volume and its worktrees stay, and universe 1 never notices."],
  rm2: ['universe rm fix/cart', "<b>rm</b> destroys universe 2's worktrees, state, generated files and data. Universe 1 is still running, and the clones still hold every commit."]
};
const LEGS = {
  1: [{x:486}, {y:-234}, {x:466}],
  2: [{x:502}, {y:-260}, {x:522}]
};
const LEGS2 = {
  1: [{x:486}, {y:-132}, {x:466}],
  2: [{x:502}, {y:-158}, {x:522}]
};

// How long each phase's command takes to type; its animation and its output
// are both offset by it, so the walkthrough can stop with the command typed.
const LEAD = {boot:1.692, new1:1.012, up1:0.978, new2:1.25, up2:1.216, down2:1.284, rm2:1.42};

// The transcript is a pure function of the timeline's time, so scrubbing and
// stepping land on the right lines with no state to keep in step.
const SHELL = [
  {l:'boot', at:0.0, k:'cmd', t:"multiversectl start acme/platform --install-only"},
  {l:'boot', at:1.992, k:'ok', t:"ui: cloned"},
  {l:'boot', at:2.542, k:'ok', t:"k8s-manifest: cloned"},
  {l:'boot', at:3.092, k:'ok', t:"api: cloned"},
  {l:'boot', at:3.642, k:'ok', t:"multiverse acme at ~/acme, manifest multiverse.yaml"},
  {l:'new1', at:0.0, k:'cmd', t:"multiversectl universe new 1"},
  {l:'new1', at:1.312, k:'ok', t:"ui: worktree on u1"},
  {l:'new1', at:1.512, k:'ok', t:"k8s-manifest: worktree on u1"},
  {l:'new1', at:1.712, k:'ok', t:"api: worktree on u1"},
  {l:'new1', at:1.912, k:'ok', t:"api: copied .env"},
  {l:'up1', at:0.0, k:'cmd', t:"multiversectl universe up 1"},
  {l:'up1', at:1.578, k:'in', t:"k8s-manifest: no compose file in its repository, so there is nothing to start"},
  {l:'up1', at:2.578, k:'ok', t:"ui up"},
  {l:'up1', at:3.378, k:'ok', t:"api up"},
  {l:'up1', at:3.728, k:'ok', t:"api: ran postUp"},
  {l:'up1', at:4.178, k:'url', t:"http://ui-u1.example.test"},
  {l:'up1', at:4.328, k:'url', t:"http://api-u1.example.test"},
  {l:'up1', at:4.878, k:'cmd', t:"curl -s ui-u1.example.test | head -1"},
  {l:'up1', at:6.328, k:'out', t:"<!doctype html><title>acme ui</title>"},
  {l:'up1', at:7.778, k:'cmd', t:"curl -s api-u1.example.test/healthz"},
  {l:'up1', at:9.228, k:'out', t:"{\"status\":\"ok\",\"universe\":\"u1\"}"},
  {l:'new2', at:0.0, k:'cmd', t:"multiversectl universe new fix/cart"},
  {l:'new2', at:1.55, k:'ok', t:"ui: new branch fix/cart from origin/main"},
  {l:'new2', at:1.75, k:'ok', t:"ui: worktree on fix/cart"},
  {l:'new2', at:1.95, k:'ok', t:"k8s-manifest: worktree on fix/cart"},
  {l:'new2', at:2.15, k:'ok', t:"api: worktree on fix/cart"},
  {l:'new2', at:2.35, k:'ok', t:"api: copied .env"},
  {l:'up2', at:0.0, k:'cmd', t:"multiversectl universe up fix/cart"},
  {l:'up2', at:1.816, k:'in', t:"k8s-manifest: no compose file in its repository, so there is nothing to start"},
  {l:'up2', at:2.816, k:'ok', t:"ui up"},
  {l:'up2', at:3.616, k:'ok', t:"api up"},
  {l:'up2', at:3.966, k:'ok', t:"api: ran postUp"},
  {l:'up2', at:4.416, k:'url', t:"http://ui-fix-cart.example.test"},
  {l:'up2', at:4.566, k:'url', t:"http://api-fix-cart.example.test"},
  {l:'up2', at:5.116, k:'cmd', t:"curl -s ui-fix-cart.example.test | head -1"},
  {l:'up2', at:6.566, k:'out', t:"<!doctype html><title>acme ui</title>"},
  {l:'up2', at:8.016, k:'cmd', t:"curl -s api-fix-cart.example.test/healthz"},
  {l:'up2', at:9.466, k:'out', t:"{\"status\":\"ok\",\"universe\":\"fix-cart\"}"},
  {l:'down2', at:0.0, k:'cmd', t:"multiversectl universe down fix/cart"},
  {l:'down2', at:2.084, k:'ok', t:"ui stopped"},
  {l:'down2', at:2.384, k:'ok', t:"api stopped"},
  {l:'rm2', at:0.0, k:'cmd', t:"multiversectl universe rm fix/cart --yes"},
  {l:'rm2', at:2.32, k:'ok', t:"ui: worktree removed"},
  {l:'rm2', at:2.52, k:'ok', t:"k8s-manifest: worktree removed"},
  {l:'rm2', at:2.77, k:'ok', t:"api: worktree removed"},
  {l:'rm2', at:3.12, k:'ok', t:"api: branch fix/cart deleted (its commits are elsewhere)"}
];
const GLYPH = {ok:['✓','ok'], in:['›','in'], wa:['!','wa']};
const esc = x => x.replace(/&/g,'&amp;').replace(/</g,'&lt;');
const TYPE = 0.034;      // seconds a character takes to appear

// Colour means "this just arrived": the box or line takes its own colour for a
// moment and the CSS fades it back to grey.
const fresh = t => gsap.timeline()
  .set(t, {attr:{'data-fresh':1}})
  .set(t, {attr:{'data-fresh':0}}, '+=1.25');

const fire = bar => gsap.fromTo(bar, {opacity:0}, {opacity:1, duration:.2, repeat:1, yoyo:true, immediateRender:false});

const tl = gsap.timeline({repeat:0, repeatDelay:.6, onUpdate:sync});
window.__tl = tl; // handle for checking the sequence from outside the page

/* Everything is drawn from the first frame as a grey outline. A command does
   not make a box appear; it fills the box in and gives it its colour, and
   `down` and `rm` empty it again. `data-on` carries that, set with attribute
   tweens so scrubbing cannot leave something half built. */
const st = (t, v) => gsap.set(t, {attr:{'data-state': v}});
const empty = t => st(t, 'empty');   // the slot: nothing here yet, or destroyed
const idle  = t => st(t, 'idle');    // it exists; nothing is running
const live  = t => st(t, 'live');    // in action, and therefore in colour
const pulse = t => gsap.fromTo(t, {scale:.96},
  {scale:1, duration:.5, ease:'back.out(2.2)', transformOrigin:'50% 50%', immediateRender:false});

/* `start`: the multiverse is read and its repositories are cloned, once. */
function phaseBoot(){
  tl.addLabel('boot')
    .to({}, {duration:LEAD.boot})
    .fromTo(['#manifest-card', '#manifest-hint'], {opacity:0}, {opacity:1, duration:.4, immediateRender:false}).add(pulse('#manifest-card'), '<')
    .fromTo('#clone-ui', {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.3').add(pulse('#clone-ui'),  '<')
    .fromTo('#clone-api', {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.3').add(pulse('#clone-api'), '<')
    .fromTo('#clone-k8s', {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.3').add(pulse('#clone-k8s'), '<')
    .to({}, {duration:1.0});
}

/* `new`: the worktrees fill in. */
function phaseNew(u){
  tl.addLabel('new' + u)
    .to({}, {duration:LEAD['new' + u]})
    .add(fire(`#u${u}-bar-new`))
    .add(idle([...[U[u].newmark].flat(), U[u].frame, ...U[u].disk]), '<')
    .fromTo([...[U[u].newmark].flat(), U[u].frame], {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '<')
    .fromTo(U[u].disk[0], {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.2').add(pulse(U[u].disk[0]), '<')
    .fromTo(U[u].disk[1], {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.2').add(pulse(U[u].disk[1]), '<')
    .fromTo(U[u].disk[2], {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.2').add(pulse(U[u].disk[2]), '<')
    .to({}, {duration:.9});
}

/* `up`: the generated files, then the projects and their services. */
function phaseUp(u){
  tl.addLabel('up' + u)
    .to({}, {duration:LEAD['up' + u]})
    .add(live([...U[u].written, U[u].frame, ...U[u].disk, `#u${u}-disk`,
               ...[U[u].newmark].flat()]))
    .fromTo(U[u].written, {opacity:0}, {opacity:1, duration:.4, immediateRender:false}).add(pulse(U[u].written), '<')
    .add(live('#proxy-box'), '<')
    .add(fire(`#u${u}-bar-up`), '+=0.25')
    .add(live([U[u].upmark, ...U[u].projects, ...U[u].services, U[u].vol,
               `#road-u${u}`, `#u${u}-dock`, '#proxy-box']), '<')
    .fromTo(U[u].upmark, {opacity:0}, {opacity:1, duration:.4, immediateRender:false})
    .fromTo(U[u].projects.slice(1), {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.1').add(pulse(U[u].projects.slice(1)), '<')
    .fromTo(U[u].services, {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.3').add(pulse(U[u].services), '<')
    .fromTo(U[u].vol, {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.1').add(pulse(U[u].vol), '<')
    .fromTo(`#road-u${u}`, {opacity:0}, {opacity:1, duration:.4, immediateRender:false}, '+=0.15')
    .fromTo(`#lbl-u${u}`, {opacity:0}, {opacity:1, duration:.3, immediateRender:false}, '<')
    // the first curl, once its own Enter has run it
    .fromTo(`#tok-u${u}`, {opacity:0, x:0, y:0}, {opacity:1, duration:.2, immediateRender:false},
            `up${u}+=${LEAD['up' + u] + 5.3}`)
    .to(`#tok-u${u}`, {...LEGS[u][0], duration:.7, ease:'power1.inOut'})
    .to(`#tok-u${u}`, {...LEGS[u][1], duration:.5, ease:'power1.inOut'})
    .to(`#tok-u${u}`, {...LEGS[u][2], duration:.18})
    .add(pulse(`#u${u}-pui`), '-=0.05')
    .to(`#tok-u${u}`, {opacity:0, duration:.18})
    // and the second, after the Enter that runs it
    .fromTo(`#tok2-u${u}`, {opacity:0, x:0, y:0}, {opacity:1, duration:.2, immediateRender:false},
            `up${u}+=${LEAD['up' + u] + 8.2}`)
    .to(`#tok2-u${u}`, {...LEGS2[u][0], duration:.7, ease:'power1.inOut'})
    .to(`#tok2-u${u}`, {...LEGS2[u][1], duration:.5, ease:'power1.inOut'})
    .to(`#tok2-u${u}`, {...LEGS2[u][2], duration:.18})
    .add(pulse(`#u${u}-papi`), '-=0.05')
    .to(`#tok2-u${u}`, {opacity:0, duration:.18})
    .to({}, {duration:.6}, `up${u}+=${LEAD['up' + u] + 9.6}`);
}

/* `down`: the services and the networks empty out; the data stays. */
function phaseDown(u){
  tl.addLabel('down' + u)
    .to({}, {duration:LEAD['down' + u]})
    .to(`#road-u${u}`, {opacity:0, duration:.3})
    .to(`#lbl-u${u}`, {opacity:0, duration:.3}, '<')
    .to(U[u].services, {opacity:0, duration:.35, stagger:.06}, '+=0.2')
    .to(U[u].projects.slice(1), {opacity:0, duration:.35, stagger:.08}, '+=0.1')
    .to(U[u].upmark, {opacity:0, duration:.3}, '<')
    .add(idle([U[u].frame, ...U[u].disk, ...U[u].written, U[u].vol,
               `#u${u}-disk`, `#u${u}-dock`]), '<')
    .fromTo(`#u${u}-voltag`, {opacity:0}, {opacity:1, duration:.3, immediateRender:false}, '<')
    .add(pulse(U[u].vol), '<')
    .to({}, {duration:1});
}

/* `rm`: everything this universe owned goes back to an empty outline. */
function phaseRm(u){
  tl.addLabel('rm' + u)
    .to({}, {duration:LEAD['rm' + u]})
    .to(`#u${u}-voltag`, {opacity:0, duration:.25})
    .to([...U[u].disk].reverse(), {opacity:0, duration:.4, stagger:.1}, '+=0.1')
    .to([...U[u].written, U[u].vol], {opacity:0, duration:.4, stagger:.06}, '-=0.2')
    .to([U[u].frame, ...[U[u].newmark].flat()], {opacity:0, duration:.4}, '-=0.1')
    .to({}, {duration:1.2});
}

phaseBoot(); phaseNew(1); phaseUp(1); phaseNew(2); phaseUp(2); phaseDown(2); phaseRm(2);

/* ---- controls --------------------------------------------------------- */
const order = ['boot', 'new1', 'up1', 'new2', 'up2', 'down2', 'rm2'].map(String);
const at = Object.fromEntries(order.map(k => [k, tl.labels[k]]));
// Every command stops with itself typed and nothing done yet — the phase's own
// command and the curls alike, so each one waits for its own Enter.
const CMDS = SHELL.filter(e => e.k === 'cmd')
  .map(e => tl.labels[e.l] + e.at + e.t.length * TYPE + 0.06)
  .sort((a, b) => a - b);
const STOPS = CMDS;
let shown = null;

// Manual is the default: you read the command, press Enter, and watch that one
// step. `auto` hands it over to the loop. `stopAt` is where the current run has
// to stop; it is checked on every frame rather than with a callback, so
// scrubbing and the step buttons cannot leave a stale one behind.
let manual = true, awaiting = true, stopAt = null, stepIdx = 0, atEnd = false;

function park(i){
  stepIdx = Math.max(0, Math.min(STOPS.length - 1, i));
  stopAt = null; atEnd = false; awaiting = true;
  tl.pause(STOPS[stepIdx]);
  sync(); paint();
}
function runStep(){
  if (atEnd) { tl.pause(0); park(0); return; }
  if (!awaiting) return;
  stopAt = stepIdx + 1 < STOPS.length ? STOPS[stepIdx + 1] : tl.duration();
  awaiting = false;
  tl.play();
  paint();
}
function reachedStop(){
  if (stopAt === null || tl.time() < stopAt - 0.001) return;
  const end = stopAt >= tl.duration() - 0.001;
  tl.pause(stopAt);
  stopAt = null; awaiting = true;
  if (end) atEnd = true; else stepIdx += 1;
  paint();
}
function setAuto(on){
  manual = !on;
  $('auto').setAttribute('aria-pressed', String(on));
  if (on) {
    stopAt = null; awaiting = false; atEnd = false;
    tl.repeat(-1); tl.play();
  } else {
    tl.repeat(0);
    park(STOPS.reduce((n, p, i) => tl.time() >= p - 0.001 ? i : n, 0));
  }
  paint();
}
function paint(){
  const waiting = manual && awaiting;
  $('shell').classList.toggle('waiting', waiting);
  $('run').disabled = !waiting && !atEnd;
  $('run').textContent = atEnd ? '↩ run it again' : '↩ run';
}

function current(){
  const t = tl.time();
  let found = order[0];
  for (const k of order) if (t >= at[k] - 0.001) found = k;
  return found;
}

const shellEl = $('shell');

// It behaves like a terminal: the whole transcript is there to scroll back
// through, and it follows the newest line only while you are already at the
// bottom — scroll up and it stays where you put it, scroll back down and it
// picks up again.
let follow = true;
shellEl.addEventListener('scroll', () => {
  follow = shellEl.scrollHeight - shellEl.scrollTop - shellEl.clientHeight < 8;
});
// Rebuilt only when a character or a line actually changed, so dragging the
// scrollbar is not fought by sixty rewrites a second.
let lastSig = '';

function renderShell(t){
  // The command that is running stays pinned; only what it prints moves. The
  // window is three lines and scrolls, so a long output can be read back.
  let cmd = null, cmdStart = 0, out = [];
  for (const e of SHELL) {
    const start = tl.labels[e.l] + e.at;
    if (t < start) continue;
    if (e.k === 'cmd') { cmd = e; cmdStart = start; out = []; continue; }
    if (e.k === 'url')      out.push(`<div>  <span class="a">${esc(e.t)}</span></div>`);
    else if (e.k === 'out') out.push(`<div><span class="d">${esc(e.t)}</span></div>`);
    else {
      const [g, cls] = GLYPH[e.k];
      out.push(`<div><span class="${cls}">${g}</span> ${esc(e.t)}</div>`);
    }
  }
  let head = '';
  let typed = 0;
  if (cmd) {
    typed = Math.min(cmd.t.length, Math.floor((t - cmdStart) / TYPE));
    const done = typed >= cmd.t.length;
    const caret = (!done || awaiting) ? '<span class="cur">█</span>' : '';
    // the command sits there typed, waiting, and says so
    const press = (awaiting && done) ? '<span class="press">press enter</span>' : '';
    head = `<span class="pr">$</span> ${esc(cmd.t.slice(0, typed))}${caret}${press}`;
  }
  const sig = (cmd ? cmd.t : '') + ':' + typed + ':' + out.length + ':' + awaiting;
  if (sig === lastSig) return;
  lastSig = sig;
  $('shell-cmd').innerHTML = head;
  shellEl.innerHTML = out.join('');
  if (follow) shellEl.scrollTop = shellEl.scrollHeight;
}

function sync(){
  reachedStop();
  renderShell(tl.time());
  const k = current();
  if (k === shown) return;
  shown = k;
  const [name, text] = steps[k];
  $('now').innerHTML = `<b>multiversectl ${name}</b> &mdash; ${text}`;

}

$('run').addEventListener('click', runStep);
$('auto').addEventListener('click', () => setAuto(manual));
// Enter runs the command that is waiting, the way it would in a real shell.
const stage = $('shell').closest('.term').parentNode;
document.addEventListener('keydown', e => {
  if (e.key !== 'Enter' || !mpanel.hidden) return;
  const el = document.activeElement;
  if (el && (el.tagName === 'BUTTON' || el.tagName === 'INPUT')) return;
  // on a page of prose, Enter belongs to the reader unless they are in here
  if (!stage.contains(el)) return;
  e.preventDefault(); runStep();
});

// Every card in the drawing with a data-file opens the same drawer, showing that
// repository's tree with one file open. The diagram stays readable behind it.
const FILES = {
 "manifest": {
  "title": "~/acme/",
  "kind": "the multiverse",
  "lede": "One file groups the repositories below it. <code>kind: Multiverse</code> is what makes them one thing that can be run several times over.",
  "note": "That is the whole of it. A repository no <code>apps:</code> entry names gets an app of its own name, so <code>ui</code> and <code>api</code> each become one app with a compose project per universe. It never describes containers &mdash; those stay in each repository, and <code>metadata.name</code> is what prefixes every compose project: <code>acme-u1-api</code>.",
  "open": "multiverse.yaml",
  "tree": [
   {
    "d": 0,
    "n": "acme/",
    "f": null
   },
   {
    "d": 1,
    "n": "multiverse.yaml",
    "f": "multiverse.yaml"
   },
   {
    "d": 1,
    "n": "ui/",
    "f": null
   },
   {
    "d": 1,
    "n": "api/",
    "f": null
   },
   {
    "d": 1,
    "n": "k8s-manifest/",
    "f": null
   },
   {
    "d": 1,
    "n": "universes/",
    "f": null
   },
   {
    "d": 2,
    "n": "1/",
    "f": null
   },
   {
    "d": 2,
    "n": "2/",
    "f": null
   }
  ],
  "files": {
   "multiverse.yaml": "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: acme\nspec:\n  expose: lan                 # reachable from your network\n  domain: auto                # hostnames follow `expose`\n  repos:\n    - name: ui\n      url: git@github.com:acme/ui.git\n    - name: k8s-manifest      # no compose file\n      url: git@github.com:acme/k8s-manifest.git\n    - name: api\n      url: git@github.com:acme/api.git"
  }
 },
 "ui": {
  "title": "ui",
  "kind": "react, hot reload",
  "lede": "An ordinary Vite dev server. Nothing here is written for multiversectl except the labels, and a label is inert metadata until something reads it &mdash; <code>docker compose up</code> by hand still works.",
  "note": "<code>host.ui: \"5173\"</code> is the whole of the routing: that service answers at <code>ui-u1.example.test</code>. <code>ready: \"/\"</code> makes <b>up</b> wait until it really answers, and <code>url:api</code> hands this universe&rsquo;s own API address to the front end, which is why a login works in every copy. The bind mount is what reloads on save &mdash; multiversectl reloads nothing itself.",
  "open": "compose.yaml",
  "tree": [
   {
    "d": 0,
    "n": "ui/",
    "f": null
   },
   {
    "d": 1,
    "n": "compose.yaml",
    "f": "compose.yaml"
   },
   {
    "d": 1,
    "n": "Dockerfile",
    "f": "Dockerfile"
   },
   {
    "d": 1,
    "n": "package.json",
    "f": null
   },
   {
    "d": 1,
    "n": "vite.config.ts",
    "f": null
   },
   {
    "d": 1,
    "n": "src/",
    "f": null
   },
   {
    "d": 2,
    "n": "main.tsx",
    "f": null
   },
   {
    "d": 2,
    "n": "App.tsx",
    "f": null
   }
  ],
  "files": {
   "compose.yaml": "services:\n  web:\n    build: .\n    command: npm run dev -- --host 0.0.0.0 --port 5173\n    volumes:\n      - .:/app                 # a save reloads\n      - /app/node_modules\n    labels:\n      dev.multiversectl.host.ui: \"5173\"\n      dev.multiversectl.host.ui.ready: \"/\"\n      dev.multiversectl.env.VITE_API_URL: url:api\n      dev.multiversectl.env.VITE_ALLOWED_HOSTS: hosts",
   "Dockerfile": "FROM node:22-alpine\nWORKDIR /app\nCOPY package*.json ./\nRUN npm ci\nCOPY . .\nEXPOSE 5173"
  }
 },
 "api": {
  "title": "api",
  "kind": "http &middot; db &middot; redis",
  "lede": "Three services, one of them hot-reloading, with a volume for the database. It also carries a <code>.multiverse.yaml</code>: the repository saying how it runs, so the multiverse does not have to.",
  "note": "The volume is declared once and becomes a different volume in every universe, which is why <b>rm</b> on one leaves the other&rsquo;s database alone. <code>.multiverse.yaml</code> is read from the universe&rsquo;s own checkout, so a change to it can be tried on a branch before it is merged; only <code>spec.apps</code> and <code>spec.proxy.entrypoints</code> are taken from it, and the leading dot keeps it from being mistaken for a multiverse of its own.",
  "open": "compose.yaml",
  "tree": [
   {
    "d": 0,
    "n": "api/",
    "f": null
   },
   {
    "d": 1,
    "n": ".multiverse.yaml",
    "f": ".multiverse.yaml"
   },
   {
    "d": 1,
    "n": ".env.example",
    "f": null
   },
   {
    "d": 1,
    "n": "compose.yaml",
    "f": "compose.yaml"
   },
   {
    "d": 1,
    "n": "Dockerfile",
    "f": null
   },
   {
    "d": 1,
    "n": "app/",
    "f": null
   },
   {
    "d": 2,
    "n": "main.py",
    "f": null
   },
   {
    "d": 1,
    "n": "migrations/",
    "f": null
   }
  ],
  "files": {
   "compose.yaml": "services:\n  http:\n    build: .\n    command: uvicorn app.main:app --host 0.0.0.0 --reload\n    volumes:\n      - .:/code                # a save reloads\n    env_file: [.env]\n    depends_on: [db, redis]\n    labels:\n      dev.multiversectl.host.api: \"8000\"\n      dev.multiversectl.host.api.entrypoint: devapi\n      dev.multiversectl.host.api.ready: /healthz\n      dev.multiversectl.env.ALLOWED_HOSTS: hosts\n      dev.multiversectl.env.CORS_ORIGINS: origins:space\n  db:\n    image: postgres:16\n    volumes:\n      - postgres_data:/var/lib/postgresql/data\n  redis:\n    image: redis:7-alpine\nvolumes:\n  postgres_data:",
   ".multiverse.yaml": "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: api\nspec:\n  proxy:\n    entrypoints:\n      - {name: devapi, port: 8001}\n  apps:\n    - services: [http, db, redis]\n      secrets: [.env]"
  }
 },
 "k8s-manifest": {
  "title": "k8s-manifest",
  "kind": "nothing to run",
  "lede": "No compose file anywhere in it. Every universe still checks it out on its own branch &mdash; it just starts nothing for it.",
  "note": "That is the point of checking it out anyway: the manifests travel on the same branch as the code they deploy, so a universe reviewing a change sees both. <b>up</b> says so rather than failing &mdash; <code>k8s-manifest: no compose file in its repository, so there is nothing to start</code> &mdash; and carries on with the apps that do have one.",
  "open": "deployment.yaml",
  "tree": [
   {
    "d": 0,
    "n": "k8s-manifest/",
    "f": null
   },
   {
    "d": 1,
    "n": "README.md",
    "f": null
   },
   {
    "d": 1,
    "n": "base/",
    "f": null
   },
   {
    "d": 2,
    "n": "deployment.yaml",
    "f": "deployment.yaml"
   },
   {
    "d": 2,
    "n": "service.yaml",
    "f": null
   },
   {
    "d": 1,
    "n": "overlays/",
    "f": null
   },
   {
    "d": 2,
    "n": "staging/",
    "f": null
   },
   {
    "d": 2,
    "n": "production/",
    "f": null
   }
  ],
  "files": {
   "deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  replicas: 2\n  template:\n    spec:\n      containers:\n        - name: api\n          image: ghcr.io/acme/api:latest\n          ports:\n            - containerPort: 8000"
  }
 }
};
const mpanel = $('file-panel'), mscrim = $('manifest-scrim'), mwrap = document.querySelector('.wrap');
let returnTo = null, openRepo = null;

// YAML highlighting is a line pass rather than a parser: a key, and a trailing
// comment. The file bodies stay plain text, which is what makes them easy to keep
// honest against the real docs.
const hl = src => esc(src).split('\n').map(line => {
  // a dev.multiversectl.* label is the only multiverse-specific line in a
  // compose file, so it is marked as such rather than as just another key
  const mv = line.match(/^(\s*)(dev\.multiversectl\.[A-Za-z0-9_.\-]+)(:)(.*)$/);
  if (mv) return `${mv[1]}<mark class="lbl">${mv[2]}</mark>${mv[3]}${mv[4]}`;
  const m = line.match(/^(\s*-?\s*)([A-Za-z0-9_.\-]+)(:)(.*)$/);
  const out = m ? `${m[1]}<span class="k">${m[2]}</span>${m[3]}${m[4]}` : line;
  return out.replace(/(#.*)$/, '<span class="c">$1</span>');
}).join('\n');

function showFile(repo, name){
  const r = FILES[repo];
  $('file-name').textContent = name;
  $('file-code').innerHTML = hl(r.files[name]);
  $('file-legend').hidden = !/dev\.multiversectl/.test(r.files[name]);
  $('file-tree').querySelectorAll('button').forEach(b =>
    b.setAttribute('aria-current', String(b.dataset.name === name)));
}
function openFiles(repo, trigger){
  const r = FILES[repo];
  if (!r) return;
  openRepo = repo;
  returnTo = trigger || returnTo;
  $('file-title').textContent = r.title;
  $('file-kind').innerHTML = r.kind;
  $('file-lede').innerHTML = r.lede;
  $('file-note').innerHTML = r.note;
  $('file-tree').innerHTML = r.tree.map(e => e.f
    ? `<li style="--d:${e.d}"><button type="button" data-name="${e.f}">${esc(e.n)}</button></li>`
    : `<li style="--d:${e.d}"><span${e.n.endsWith('/') ? ' class="dir"' : ''}>${esc(e.n)}</span></li>`
  ).join('');
  $('file-tree').querySelectorAll('button').forEach(b =>
    b.addEventListener('click', () => showFile(repo, b.dataset.name)));
  showFile(repo, r.open);
  $('file-legend').hidden = !/dev\.multiversectl/.test(Object.values(r.files).join(''));
  setPanel(true);
}
function setPanel(open){
  if (open) {
    mscrim.hidden = false; mpanel.hidden = false;
    // flush the layout so the transition has a start state to run from; a
    // requestAnimationFrame would be throttled in a background tab and the
    // drawer would then be present without ever sliding in
    void mpanel.offsetWidth;
  }
  mscrim.classList.toggle('open', open);
  mpanel.classList.toggle('open', open);
  document.querySelectorAll('[data-file]').forEach(c =>
    c.setAttribute('aria-expanded', String(open && c.dataset.file === openRepo)));
  mwrap.inert = open;
  if (open) $('file-close').focus();
  else {
    const hide = () => { mscrim.hidden = true; mpanel.hidden = true; };
    matchMedia('(prefers-reduced-motion: reduce)').matches ? hide() : setTimeout(hide, 280);
    if (returnTo) returnTo.focus();
  }
}
document.querySelectorAll('[data-file]').forEach(card => {
  const repo = card.dataset.file;
  const toggle = () => (!mpanel.hidden && openRepo === repo) ? setPanel(false) : openFiles(repo, card);
  card.addEventListener('click', toggle);
  card.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') { e.stopPropagation(); e.preventDefault(); toggle(); }
  });
});
$('file-close').addEventListener('click', () => setPanel(false));
mscrim.addEventListener('click', () => setPanel(false));
document.addEventListener('keydown', e => {
  if (e.key === 'Escape' && !mpanel.hidden) setPanel(false);
  if (e.key === 'Tab' && !mpanel.hidden) {
    const f = mpanel.querySelectorAll('button, [href], [tabindex]:not([tabindex="-1"])');
    if (!f.length) return;
    const first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
});

$('theme').addEventListener('click', () => {
  const dark = getComputedStyle(document.body).backgroundColor === 'rgb(17, 20, 27)';
  document.documentElement.dataset.theme = dark ? 'light' : 'dark';
});

// It opens with the first command typed and waiting, which is the same state a
// reader who asked for less motion wants: nothing moves until they ask.
park(0);
