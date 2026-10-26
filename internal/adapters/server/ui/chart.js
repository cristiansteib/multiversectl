'use strict';

// Line charts, drawn as SVG with the DOM: the page builds no markup from
// strings, here no more than anywhere else, and a series name is a container's
// name, which nobody vouches for.
//
// The rules this follows, because a chart that breaks them misleads: one scale
// per chart and never two; a colour belongs to a thing, not to its place in a
// sorted list, so a filter never repaints what is left; past eight things the
// rest are one grey "other" rather than a ninth made-up colour; and every value
// the hover shows is in the table below it too.
const viz = (() => {
  const NS = 'http://www.w3.org/2000/svg';
  const SLOTS = 8;

  const el = (name, attrs, ...kids) => {
    const n = document.createElementNS(NS, name);
    for (const [k, v] of Object.entries(attrs || {})) if (v != null) n.setAttribute(k, String(v));
    for (const kid of kids.flat(Infinity)) if (kid != null) n.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
    return n;
  };

  // A palette keeps one colour per key for as long as that key is around, so
  // the line a reader has learned does not become another thing's line.
  function palette() {
    const held = new Map();
    return {
      // claim gives the keys their colours in the order they are offered, up
      // to eight; everything after shares the grey of "other".
      claim(keys) {
        for (const k of held.keys()) if (!keys.includes(k)) held.delete(k);
        const taken = new Set(held.values());
        for (const k of keys) {
          if (held.has(k)) continue;
          let free = 0;
          while (free < SLOTS && taken.has(free)) free++;
          if (free >= SLOTS) break;
          held.set(k, free);
          taken.add(free);
        }
      },
      has: (k) => held.has(k),
      // A class, not a colour: the page's stylesheet holds the palette, and
      // its content security policy allows no style of its own anywhere.
      of: (k) => (held.has(k) ? 's' + (held.get(k) + 1) : 'so'),
      size: () => held.size,
    };
  }

  // Numbers people read: four significant digits are noise on an axis, and a
  // gridline must say what it is on, so 1.25 GiB is not rounded to 1.3.
  const trim = (s) => (s.includes('.') ? s.replace(/\.?0+$/, '') : s);
  const bytes = (n) => {
    if (!n) return '0';
    const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return trim(n >= 100 || i === 0 ? String(Math.round(n)) : n >= 10 ? n.toFixed(1) : n.toFixed(2)) + ' ' + u[i];
  };
  const percent = (n) => (!n ? '0' : trim(n >= 100 ? String(Math.round(n)) : n >= 10 ? n.toFixed(0) : n.toFixed(1))) + '%';
  const clock = (t) => new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });

  // A scale whose gridlines land on numbers worth printing: a step of a round
  // size, and a top that is a multiple of it just above the highest value. A
  // base of 1024 keeps memory on 256 MiB and 1 GiB rather than on 250 MB.
  const STEPS = [0.1, 0.125, 0.2, 0.25, 0.5, 1, 2, 2.5, 5];
  function scale(max, base) {
    if (!(max > 0)) return base === 1024 ? { top: 1024, step: 256 } : { top: 1, step: 0.25 };
    const b = base || 10;
    const mag = b ** Math.floor(Math.log(max) / Math.log(b));
    for (const m of STEPS) {
      const step = m * mag;
      const n = Math.ceil(max / step);
      if (n >= 3 && n <= 5) return { top: n * step, step };
    }
    return { top: max, step: max / 4 };
  }

  // Time ticks at a round age counted back from now, so they read as 1m and 2m
  // and not as whatever a quarter of the window happens to be.
  const TIME_STEPS = [5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];
  function timeTicks(from, to) {
    const span = (to - from) / 1000;
    const step = TIME_STEPS.find((x) => span / x <= 6) || 3600;
    const out = [];
    for (let secs = 0; secs <= span + 0.5; secs += step) out.push(secs);
    return out;
  }

  const MINUS = '\u2212';
  const age = (secs) => (secs < 1 ? 'now' : secs < 60 ? MINUS + secs + 's'
    : MINUS + (secs % 60 ? (secs / 60).toFixed(1) : secs / 60) + 'm');

  // chart draws one plot. series is [{ key, label, cls, points: [[t, v]] }]
  // over the window [from, to]; every series shares the one y scale.
  //
  // opts: { width, height, format, base, floor, label, compact, onhover }
  function chart(series, from, to, opts) {
    const o = Object.assign({ width: 640, height: 190, format: percent, label: '', compact: false }, opts);
    const pad = o.compact
      ? { t: 6, r: 8, b: 14, l: 38 }
      : { t: 10, r: 14, b: 20, l: 52 };
    const w = Math.max(160, o.width), h = o.height;
    const iw = w - pad.l - pad.r, ih = h - pad.t - pad.b;
    let peak = o.floor || 0;
    for (const s of series) for (const p of s.points) if (p[1] > peak) peak = p[1];
    // floor is how small multiples share one scale: side by side they are
    // compared, and two charts of the same height that mean different things
    // are worse than no charts.
    const sc = scale(peak, o.base);
    const top = sc.top;
    const span = Math.max(1, to - from);
    const x = (t) => pad.l + (iw * (t - from)) / span;
    const y = (v) => pad.t + ih - (ih * Math.min(v, top)) / top;

    const svg = el('svg', {
      class: 'plot', width: w, height: h, viewBox: `0 0 ${w} ${h}`, role: 'img', tabindex: '0',
      'aria-label': (o.label ? o.label + '. ' : '') + series.map((s) => s.label + ' ' + o.format(s.points.length ? s.points[s.points.length - 1][1] : 0)).join(', '),
    });

    const grid = el('g', { class: 'grid' });
    for (let v = 0; v <= top + sc.step / 2; v += sc.step) {
      // A small plot says only where the floor and the ceiling are.
      if (o.compact && v > 0 && Math.abs(v - top) > sc.step / 2) continue;
      grid.append(el('line', { x1: pad.l, x2: w - pad.r, y1: y(v), y2: y(v) }));
      grid.append(el('text', { x: pad.l - 6, y: y(v) + 4, 'text-anchor': 'end', class: 'axis' }, o.format(v)));
    }
    svg.append(grid);

    if (!o.compact) {
      const times = el('g', { class: 'axis-x' });
      for (const secs of timeTicks(from, to)) {
        const at = x(to - secs * 1000);
        if (at < pad.l - 1) continue;
        times.append(el('text', {
          x: at, y: h - 6, class: 'axis',
          'text-anchor': secs === 0 ? 'end' : at < pad.l + 16 ? 'start' : 'middle',
        }, age(secs)));
      }
      svg.append(times);
    }

    for (const s of series) {
      if (s.points.length < 2) continue;
      const d = s.points.map((p, i) => (i ? 'L' : 'M') + x(p[0]).toFixed(1) + ' ' + y(p[1]).toFixed(1)).join(' ');
      svg.append(el('path', { d, class: 'line ' + s.cls, fill: 'none' }));
      const last = s.points[s.points.length - 1];
      svg.append(el('circle', { cx: x(last[0]), cy: y(last[1]), r: 2.5, class: 'head ' + s.cls }));
    }

    const hair = el('line', { class: 'hair', y1: pad.t, y2: pad.t + ih, x1: 0, x2: 0, opacity: 0 });
    svg.append(hair);

    // The crosshair finds the time, never the line: a reader aims at a moment
    // and is told what every series was doing then.
    const at = (clientX) => {
      const box = svg.getBoundingClientRect();
      const t = from + (span * (clientX - box.left - pad.l)) / iw;
      return Math.min(to, Math.max(from, t));
    };
    const show = (t) => {
      hair.setAttribute('x1', x(t));
      hair.setAttribute('x2', x(t));
      hair.setAttribute('opacity', 1);
      if (o.onhover) o.onhover(series.map((s) => ({ s, p: nearest(s.points, t) })).filter((r) => r.p), t);
    };
    const hide = () => { hair.setAttribute('opacity', 0); if (o.onhover) o.onhover(null); };
    svg.addEventListener('pointermove', (e) => show(at(e.clientX)));
    svg.addEventListener('pointerleave', hide);
    svg.addEventListener('blur', hide);
    let cursor = null;
    svg.addEventListener('keydown', (e) => {
      const all = series.flatMap((s) => s.points.map((p) => p[0])).sort((a, b) => a - b);
      if (!all.length) return;
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        const i = cursor == null ? all.length - 1 : all.indexOf(cursor);
        cursor = all[Math.min(all.length - 1, Math.max(0, (i < 0 ? all.length - 1 : i) + (e.key === 'ArrowRight' ? 1 : -1)))];
        show(cursor);
        e.preventDefault();
      } else if (e.key === 'Escape') { cursor = null; hide(); }
    });
    return svg;
  }

  function nearest(points, t) {
    let best = null, gap = Infinity;
    for (const p of points) {
      const d = Math.abs(p[0] - t);
      if (d < gap) { gap = d; best = p; }
    }
    return best;
  }

  // sparkline is the same shape without any chrome: it belongs beside a number
  // in a table, where it says "and this is where it came from".
  function sparkline(points, from, to, cls, w, h) {
    const svg = el('svg', { class: 'spark ' + (cls || ''), width: w || 90, height: h || 20, viewBox: `0 0 ${w || 90} ${h || 20}`, 'aria-hidden': 'true' });
    if (points.length < 2) return svg;
    let peak = 0;
    for (const p of points) if (p[1] > peak) peak = p[1];
    const top = scale(peak).top, span = Math.max(1, to - from);
    const d = points.map((p, i) => (i ? 'L' : 'M') + (((w || 90) * (p[0] - from)) / span).toFixed(1) + ' ' + ((h || 20) - 2 - ((h || 20) - 4) * (p[1] / top)).toFixed(1)).join(' ');
    svg.append(el('path', { d, fill: 'none', 'stroke-width': 1.5, 'stroke-linejoin': 'round' }));
    return svg;
  }

  return { chart, sparkline, palette, bytes, percent, clock, el };
})();
