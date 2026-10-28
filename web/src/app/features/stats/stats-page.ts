import { ChangeDetectionStrategy, Component, DestroyRef, HostListener, computed, effect, inject, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Api } from '../../core/api.service';
import { FleetStatRow, FleetStatsView } from '../../core/models';
import { plural, universeLink } from '../../core/util';
import { bytes, clock, percent } from '../../shared/chart/chart-math';
import { ChartSeries, LineChart } from '../../shared/chart/line-chart';
import { Palette, otherStyle } from '../../shared/chart/palette';
import { Sparkline } from '../../shared/chart/sparkline';
import { Badge } from '../../shared/ui/badge';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';

interface Sample { t: number; rows: FleetStatRow[] }
type Group = 'universe' | 'app' | 'service';

const WINDOWS: [number, string][] = [[120_000, '2 minutes'], [300_000, '5 minutes'], [900_000, '15 minutes']];
const EVERY: [number, string][] = [[2000, 'every 2 s'], [5000, 'every 5 s'], [10_000, 'every 10 s'], [30_000, 'every 30 s']];
const MAX_SAMPLES = 600;
const OTHER = '\u0000other';

const uniOf = (r: FleetStatRow): string => `${r.multiverse}/${r.universe}`;
const svcOf = (r: FleetStatRow): string => `${uniOf(r)}/${r.app}/${r.service}`;
const natural = (a: string, b: string): number => a.localeCompare(b, undefined, { numeric: true });

/**
 * What every running container of every universe is using, read over and over
 * and drawn as lines. The server keeps no history — one reading is all it has —
 * so the window is what this page has seen since it was opened, and the page
 * says so.
 */
@Component({
  selector: 'mv-stats-page',
  imports: [RouterLink, LineChart, Sparkline, Badge, EmptyState, ErrorBox, Icon, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './stats-page.html',
  styleUrl: './stats-page.css',
})
export class StatsPage {
  private readonly api = inject(Api);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly windows = WINDOWS;
  protected readonly everies = EVERY;
  protected readonly bytes = bytes;
  protected readonly percent = percent;
  protected readonly clock = clock;
  protected readonly plural = plural;
  protected readonly link = universeLink;
  protected readonly svcOf = svcOf;

  // What has been read, newest last: one entry per reading.
  protected readonly samples = signal<Sample[]>([]);
  protected readonly group = signal<Group>('universe');
  protected readonly span = signal(300_000);
  protected readonly every = signal(2000);
  protected readonly paused = signal(false);
  protected readonly failed = signal<{ multiverse: string; error: string }[]>([]);
  protected readonly menuOpen = signal(false);
  // Which universes are left out, rather than which are in: a universe that
  // appears while the page is open is shown without being asked for.
  protected readonly hidden = signal<ReadonlySet<string>>(new Set());

  private readonly colours = new Palette();
  private timer: ReturnType<typeof setInterval> | undefined;
  private busy = false;

  constructor() {
    effect(() => {
      const paused = this.paused();
      const every = this.every();
      untracked(() => this.arm(paused, every));
    });
    const visible = () => { if (!document.hidden && !this.paused()) void this.load(); };
    document.addEventListener('visibilitychange', visible);
    this.destroyRef.onDestroy(() => {
      clearInterval(this.timer);
      document.removeEventListener('visibilitychange', visible);
    });
    void this.load();
  }

  // ---- reading

  private arm(paused: boolean, every: number): void {
    clearInterval(this.timer);
    this.timer = paused ? undefined : setInterval(() => { if (!document.hidden) void this.load(); }, every);
  }

  protected async load(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      const d = await this.api.get<FleetStatsView>('stats');
      const t = Date.parse(d.at) || Date.now();
      // Nothing older than the longest window is kept: it can never be drawn.
      const oldest = Date.now() - WINDOWS[WINDOWS.length - 1][0];
      this.samples.update((s) => {
        const next = [...s, { t, rows: d.rows ?? [] }];
        while (next.length && (next[0].t < oldest || next.length > MAX_SAMPLES)) next.shift();
        return next;
      });
      this.failed.set(d.sources ?? []);
    } catch (e) {
      this.failed.set([{ multiverse: 'This machine', error: (e as Error).message }]);
    } finally {
      this.busy = false;
    }
  }

  protected togglePause(): void { this.paused.update((p) => !p); }

  // ---- what is shown

  private readonly shown = (r: FleetStatRow): boolean => !this.hidden().has(uniOf(r));
  protected readonly last = computed(() => this.samples().at(-1) ?? null);
  protected readonly to = computed(() => this.last()?.t ?? Date.now());
  /** The readings inside the window, with the universes that are ticked off taken out: everything drawn goes through here, so the numbers agree. */
  private readonly windowed = computed(() => {
    const from = this.to() - this.span();
    return this.samples().filter((s) => s.t >= from).map((s) => ({ t: s.t, rows: s.rows.filter(this.shown) }));
  });
  protected readonly from = computed(() => {
    const w = this.windowed();
    return w.length ? Math.max(w[0].t, this.to() - this.span()) : this.to() - this.span();
  });
  /** How long this page has been watching, when that is less than the window. */
  protected readonly watching = computed(() => {
    const s = this.samples();
    const secs = s.length ? Math.round((this.to() - s[0].t) / 1000) : 0;
    return secs * 1000 < this.span() - 1000 ? secs : null;
  });
  protected readonly rows = computed(() => (this.last()?.rows ?? []).filter(this.shown));

  protected readonly totals = computed(() => {
    const rows = this.rows();
    const busiest = rows.slice().sort((a, b) => b.cpu - a.cpu)[0];
    return {
      count: rows.length,
      universes: new Set(rows.map(uniOf)).size,
      cpu: rows.reduce((n, r) => n + r.cpu, 0),
      mem: rows.reduce((n, r) => n + (r.mem ?? 0), 0),
      busiest,
    };
  });

  /** Every universe seen within the window, so one that has just stopped can still be ticked off rather than vanish from under the pointer. */
  protected readonly universes = computed(() => {
    this.model();
    const from = this.to() - this.span();
    const seen = new Map<string, FleetStatRow>();
    for (const s of this.samples()) if (s.t >= from) for (const r of s.rows) seen.set(uniOf(r), r);
    return [...seen.entries()].sort((a, b) => natural(a[0], b[0])).map(([key, r]) => ({
      key, universe: r.universe, multiverse: r.multiverse,
      colour: this.group() === 'universe' ? this.colours.style(key).colour : null,
    }));
  });
  private readonly manyMultiverses = computed(() => new Set(this.samples().flatMap((s) => s.rows.map((r) => r.multiverse))).size > 1);
  protected readonly pickerLabel = computed(() => {
    const all = this.universes().length;
    const n = this.universes().filter((u) => !this.hidden().has(u.key)).length;
    return !this.samples().length ? '…' : all && n === all ? 'all' : `${n} of ${all}`;
  });

  private keyOf(r: FleetStatRow): string {
    const g = this.group();
    return g === 'universe' ? uniOf(r) : g === 'app' ? `${uniOf(r)}/${r.app}` : svcOf(r);
  }
  // With one multiverse its name adds nothing to every label; with two it is
  // what tells one multiverse's universe 1 from the other's.
  private labelOf(r: FleetStatRow): string {
    const uni = (this.manyMultiverses() ? `${r.multiverse} · ` : '') + r.universe;
    const g = this.group();
    return g === 'universe' ? uni : g === 'app' ? `${uni} ${r.app}` : `${uni} ${r.app}/${r.service}`;
  }

  /**
   * One line per group, summed at each reading. Everything past the eighth group
   * is one line called "more": a ninth colour nobody could tell apart would be
   * worse than saying "the rest". The palette is held between readings so a line
   * keeps its colour while it is on screen.
   */
  protected readonly model = computed(() => {
    const within = this.windowed();
    const labels = new Map<string, string>();
    const order: string[] = [];
    for (const s of within) for (const r of s.rows) {
      const k = this.keyOf(r);
      if (!labels.has(k)) { labels.set(k, this.labelOf(r)); order.push(k); }
    }
    const live = new Set((within.at(-1)?.rows ?? []).map((r) => this.keyOf(r)));
    // Those still running keep their colour first; a group that has gone quiet
    // does not take one from a group that is busy.
    this.colours.claim([...order].sort((a, b) => Number(live.has(b)) - Number(live.has(a)) || a.localeCompare(b)));
    const named = order.filter((k) => this.colours.has(k));
    const rest = order.filter((k) => !this.colours.has(k));

    const build = (field: 'cpu' | 'mem'): ChartSeries[] => {
      const series: (ChartSeries & { points: [number, number][] })[] = named.map((k) => ({ key: k, label: labels.get(k)!, ...this.colours.style(k), points: [] }));
      if (rest.length) series.push({ key: OTHER, label: `${rest.length} more`, ...otherStyle, points: [] });
      for (const s of within) {
        const sum = new Map<string, number>();
        for (const r of s.rows) {
          const k = this.colours.has(this.keyOf(r)) ? this.keyOf(r) : OTHER;
          sum.set(k, (sum.get(k) ?? 0) + (r[field] ?? 0));
        }
        for (const line of series) line.points.push([s.t, sum.get(line.key) ?? 0]);
      }
      return series;
    };
    return { cpu: build('cpu'), mem: build('mem') };
  });
  protected readonly hasReadings = computed(() => this.windowed().some((s) => s.rows.length));

  /** One small plot per universe, side by side and on one scale: past a handful of lines in one plot, the answer is more plots. */
  protected readonly facets = computed(() => {
    const by = new Map<string, { universe: string; multiverse: string; at: Map<number, number> }>();
    for (const s of this.windowed()) for (const r of s.rows) {
      const k = uniOf(r);
      const g = by.get(k) ?? { universe: r.universe, multiverse: r.multiverse, at: new Map() };
      g.at.set(s.t, (g.at.get(s.t) ?? 0) + r.cpu);
      by.set(k, g);
    }
    if (by.size < 2) return null;
    let peak = 0;
    for (const g of by.values()) for (const v of g.at.values()) peak = Math.max(peak, v);
    const live = new Set(this.rows().map(uniOf));
    return {
      peak,
      cards: [...by.entries()].sort((a, b) => natural(a[0], b[0])).map(([key, g]) => {
        const points = [...g.at.entries()].sort((a, b) => a[0] - b[0]);
        const series: ChartSeries[] = [{ key, label: g.universe, slot: 1, colour: 'var(--accent)', dash: '', points }];
        return { key, universe: g.universe, multiverse: g.multiverse, series, current: points.at(-1)?.[1] ?? 0, live: live.has(key) };
      }),
    };
  });

  /** The table is the chart's other half: every value a line holds is here as a number, in an order that does not move while it is read. */
  protected readonly table = computed(() => {
    const history = new Map<string, number[]>();
    for (const s of this.windowed()) for (const r of s.rows) {
      const k = svcOf(r);
      history.set(k, [...(history.get(k) ?? []), r.cpu]);
    }
    return this.rows().slice().sort((a, b) =>
      natural(a.multiverse, b.multiverse) || natural(a.universe, b.universe) || natural(a.app, b.app) || natural(a.service, b.service))
      .map((r) => ({ r, history: history.get(svcOf(r)) ?? [], style: this.colours.has(this.keyOf(r)) ? this.colours.style(this.keyOf(r)) : null }));
  });

  // ---- the universe picker

  protected setShown(key: string, on: boolean): void {
    this.hidden.update((h) => { const n = new Set(h); if (on) n.delete(key); else n.add(key); return n; });
  }
  protected showAll(): void { this.hidden.set(new Set()); }
  protected showNone(): void { this.hidden.set(new Set(this.universes().map((u) => u.key))); }

  @HostListener('document:click', ['$event'])
  protected outside(e: Event): void {
    if (this.menuOpen() && !(e.target as Element).closest('.picker')) this.menuOpen.set(false);
  }
  @HostListener('document:keydown.escape')
  protected escape(): void { this.menuOpen.set(false); }

  protected asNumber(v: string): number { return Number(v); }
}
