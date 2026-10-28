// The numbers a chart prints. Four significant digits are noise on an axis, and
// a gridline must say what it is on, so 1.25 GiB is not rounded to 1.3.

const trim = (s: string): string => (s.includes('.') ? s.replace(/\.?0+$/, '') : s);

export function bytes(n: number): string {
  if (!n) return '0';
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return trim(n >= 100 || i === 0 ? String(Math.round(n)) : n >= 10 ? n.toFixed(1) : n.toFixed(2)) + ' ' + u[i];
}

export function percent(n: number): string {
  return (!n ? '0' : trim(n >= 100 ? String(Math.round(n)) : n >= 10 ? n.toFixed(0) : n.toFixed(1))) + '%';
}

export const clock = (t: number): string => new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });

const STEPS = [0.1, 0.125, 0.2, 0.25, 0.5, 1, 2, 2.5, 5];

/**
 * A scale whose gridlines land on numbers worth printing: a step of a round
 * size, and a top that is a multiple of it just above the highest value. A base
 * of 1024 keeps memory on 256 MiB and 1 GiB rather than on 250 MB.
 */
export function scale(max: number, base = 10): { top: number; step: number } {
  if (!(max > 0)) return base === 1024 ? { top: 1024, step: 256 } : { top: 1, step: 0.25 };
  const mag = base ** Math.floor(Math.log(max) / Math.log(base));
  for (const m of STEPS) {
    const step = m * mag;
    const n = Math.ceil(max / step);
    if (n >= 3 && n <= 5) return { top: n * step, step };
  }
  return { top: max, step: max / 4 };
}

const TIME_STEPS = [5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];

/** Time ticks at a round age counted back from now, so they read as 1m and 2m and not as a quarter of the window. */
export function timeTicks(from: number, to: number): number[] {
  const span = (to - from) / 1000;
  const step = TIME_STEPS.find((x) => span / x <= 6) ?? 3600;
  const out: number[] = [];
  for (let secs = 0; secs <= span + 0.5; secs += step) out.push(secs);
  return out;
}

const MINUS = '−';
export const age = (secs: number): string =>
  secs < 1 ? 'now' : secs < 60 ? MINUS + secs + 's' : MINUS + (secs % 60 ? (secs / 60).toFixed(1) : secs / 60) + 'm';

/** The reading of a series nearest to a time. */
export function nearest(points: readonly (readonly [number, number])[], t: number): readonly [number, number] | null {
  let best: readonly [number, number] | null = null;
  let gap = Infinity;
  for (const p of points) {
    const d = Math.abs(p[0] - t);
    if (d < gap) { gap = d; best = p; }
  }
  return best;
}
