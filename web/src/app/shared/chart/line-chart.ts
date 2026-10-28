import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, afterNextRender, computed, inject, input, signal } from '@angular/core';
import { age, clock, nearest, percent, scale, timeTicks } from './chart-math';
import { SeriesStyle } from './palette';

export interface ChartSeries extends SeriesStyle {
  key: string;
  label: string;
  /** [time in ms, value] */
  points: readonly (readonly [number, number])[];
}

/**
 * Lines over time on one scale, with a crosshair that finds the time (never the
 * line: a reader aims at a moment and is told what every series was doing then),
 * a legend that switches series off, and every value in a table for those who do
 * not see the plot.
 */
@Component({
  selector: 'mv-line-chart',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './line-chart.html',
  styleUrl: './line-chart.css',
})
export class LineChart {
  readonly series = input.required<readonly ChartSeries[]>();
  readonly from = input.required<number>();
  readonly to = input.required<number>();
  readonly format = input<(n: number) => string>(percent);
  /** 1024 puts memory's gridlines on 256 MiB and 1 GiB. */
  readonly base = input(10);
  readonly height = input(190);
  /** A least value for the top of the scale: how small multiples share one scale. */
  readonly floor = input(0);
  /** A small plot says only where the floor and the ceiling are, and has no legend. */
  readonly compact = input(false);
  readonly label = input('');

  protected readonly width = signal(600);
  protected readonly off = signal<ReadonlySet<string>>(new Set());
  protected readonly cursor = signal<number | null>(null);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  private readonly destroyRef = inject(DestroyRef);

  constructor() {
    afterNextRender(() => {
      const ro = new ResizeObserver((e) => this.width.set(Math.max(160, Math.round(e[0].contentRect.width))));
      ro.observe(this.host.nativeElement);
      this.destroyRef.onDestroy(() => ro.disconnect());
    });
  }

  protected readonly pad = computed(() => (this.compact() ? { t: 6, r: 8, b: 6, l: 40 } : { t: 10, r: 14, b: 22, l: 54 }));
  protected readonly visible = computed(() => this.series().filter((s) => !this.off().has(s.key)));
  private readonly geo = computed(() => {
    const p = this.pad();
    const iw = this.width() - p.l - p.r;
    const ih = this.height() - p.t - p.b;
    let peak = this.floor();
    for (const s of this.visible()) for (const pt of s.points) if (pt[1] > peak) peak = pt[1];
    const sc = scale(peak, this.base());
    const span = Math.max(1, this.to() - this.from());
    return { iw, ih, sc, span };
  });
  protected readonly x = (t: number): number => this.pad().l + (this.geo().iw * (t - this.from())) / this.geo().span;
  protected readonly y = (v: number): number => {
    const g = this.geo();
    return this.pad().t + g.ih - (g.ih * Math.min(v, g.sc.top)) / g.sc.top;
  };

  protected readonly yTicks = computed(() => {
    const { sc } = this.geo();
    const out: { v: number; y: number; text: string }[] = [];
    for (let v = 0; v <= sc.top + sc.step / 2; v += sc.step) {
      // A small plot says only where the floor and the ceiling are.
      if (this.compact() && v > 0 && Math.abs(v - sc.top) > sc.step / 2) continue;
      out.push({ v, y: this.y(v), text: this.format()(v) });
    }
    return out;
  });

  protected readonly xTicks = computed(() => {
    if (this.compact()) return [];
    const l = this.pad().l;
    const out: { x: number; text: string; anchor: string }[] = [];
    for (const secs of timeTicks(this.from(), this.to())) {
      const x = this.x(this.to() - secs * 1000);
      if (x < l - 1) continue;
      out.push({ x, text: age(secs), anchor: secs === 0 ? 'end' : x < l + 16 ? 'start' : 'middle' });
    }
    return out;
  });

  protected readonly lines = computed(() =>
    this.visible().filter((s) => s.points.length >= 2).map((s) => {
      const last = s.points[s.points.length - 1];
      return {
        s,
        d: s.points.map((p, i) => `${i ? 'L' : 'M'}${this.x(p[0]).toFixed(1)} ${this.y(p[1]).toFixed(1)}`).join(''),
        hx: this.x(last[0]),
        hy: this.y(last[1]),
      };
    }));

  protected readonly summary = computed(() => {
    const f = this.format();
    const s = this.visible().map((x) => `${x.label} ${f(x.points.at(-1)?.[1] ?? 0)}`).join(', ');
    return (this.label() ? this.label() + '. ' : '') + (s || 'No series shown');
  });

  protected readonly latest = (s: ChartSeries): number => s.points.at(-1)?.[1] ?? 0;
  protected readonly peak = (s: ChartSeries): number => s.points.reduce((m, p) => Math.max(m, p[1]), 0);

  protected readonly hits = computed(() => {
    const t = this.cursor();
    if (t === null) return [];
    return this.visible()
      .map((s) => ({ s, p: nearest(s.points, t) }))
      .filter((h): h is { s: ChartSeries; p: readonly [number, number] } => h.p !== null)
      .sort((a, b) => b.p[1] - a.p[1])
      .slice(0, 6);
  });
  protected readonly cursorX = computed(() => (this.cursor() === null ? 0 : this.x(this.cursor()!)));
  protected readonly cursorTime = computed(() => (this.cursor() === null ? '' : clock(this.cursor()!)));
  protected readonly flip = computed(() => this.cursorX() > this.width() * 0.6);

  protected toggle(key: string): void {
    this.off.update((o) => {
      const n = new Set(o);
      if (!n.delete(key)) n.add(key);
      return n;
    });
  }

  protected move(e: PointerEvent): void {
    const box = (e.currentTarget as SVGElement).getBoundingClientRect();
    const g = this.geo();
    const t = this.from() + (g.span * (e.clientX - box.left - this.pad().l)) / g.iw;
    this.cursor.set(Math.min(this.to(), Math.max(this.from(), t)));
  }

  /** Arrow keys step through the moments there are readings for. */
  protected key(e: KeyboardEvent): void {
    if (e.key === 'Escape') { this.cursor.set(null); return; }
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    const all = [...new Set(this.visible().flatMap((s) => s.points.map((p) => p[0])))].sort((a, b) => a - b);
    if (!all.length) return;
    const cur = this.cursor();
    const i = cur === null ? all.length - 1 : all.findIndex((t) => t >= cur);
    const next = Math.min(all.length - 1, Math.max(0, (i < 0 ? all.length - 1 : i) + (e.key === 'ArrowRight' ? 1 : -1)));
    this.cursor.set(all[next]);
    e.preventDefault();
  }
}
