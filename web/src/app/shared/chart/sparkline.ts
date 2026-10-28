import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

const W = 120;
const H = 28;
const PAD = 2;

/** A small line of recent readings, with no axes: the shape of the last minutes, for a row or a tile. */
@Component({
  selector: 'mv-sparkline',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg [attr.viewBox]="'0 0 ' + w + ' ' + h" preserveAspectRatio="none" role="img" [attr.aria-label]="label() || 'Recent readings'">
      @if (area()) { <path class="area" [attr.d]="area()" [style.fill]="colour() || 'var(--accent)'" /> }
      @if (line()) { <path class="line" [attr.d]="line()" [style.stroke]="colour() || 'var(--accent)'" /> }
    </svg>`,
  styles: `
    :host { display: inline-block; width: 120px; height: 28px; vertical-align: middle }
    svg { width: 100%; height: 100%; display: block; overflow: visible }
    .line { fill: none; stroke-width: 1.6; stroke-linejoin: round; stroke-linecap: round; vector-effect: non-scaling-stroke }
    .area { opacity: .14; stroke: none }`,
})
export class Sparkline {
  readonly values = input.required<number[]>();
  /** The top of the scale; by default the largest reading, so a flat line stays flat. */
  readonly max = input<number>();
  readonly label = input<string>();
  readonly colour = input<string>();
  protected readonly w = W;
  protected readonly h = H;

  private readonly pts = computed(() => {
    const v = this.values().filter((x) => Number.isFinite(x));
    if (v.length < 2) return [] as [number, number][];
    const top = Math.max(this.max() ?? 0, ...v, 1e-9);
    const step = (W - 2 * PAD) / (v.length - 1);
    return v.map((x, i): [number, number] => [PAD + i * step, H - PAD - (Math.min(x, top) / top) * (H - 2 * PAD)]);
  });
  protected readonly line = computed(() => this.pts().map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join(''));
  protected readonly area = computed(() => {
    const p = this.pts();
    return p.length ? `${this.line()}L${p[p.length - 1][0].toFixed(1)} ${H}L${p[0][0].toFixed(1)} ${H}Z` : '';
  });
}
