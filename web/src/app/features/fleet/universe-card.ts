import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Events } from '../../core/events.service';
import { PulseUniverse, UniverseView } from '../../core/models';
import { universeLink } from '../../core/util';
import { bytes } from '../../shared/chart/chart-math';
import { Sparkline } from '../../shared/chart/sparkline';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { Icon } from '../../shared/ui/icon';
import { dirtyCheckouts, expectedOf, runState } from './universe-state';

/**
 * One universe as a small tile, and a link to its page: the number people say
 * out loud ("is 14 up?"), whether it is, and only what needs a look. Everything
 * else is one click away, on the universe's own page.
 */
@Component({
  selector: 'mv-universe-card',
  imports: [RouterLink, Sparkline, Icon, AgoPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <a class="tile" [class.stopped]="state() === 'stopped'" [routerLink]="link()">
      @if (edited() || universe().created) {
        <span class="wtrow">
          @if (universe().created) { <span class="wt" [attr.title]="'Created ' + universe().created">{{ universe().created | ago }}</span> }
          @if (edited()) { <span class="wt dirty" [attr.title]="'uncommitted changes in ' + dirty().join(', ')">{{ dirty().length }} changed</span> }
        </span>
      }
      <span class="num" [class.word]="!isNumber()" [attr.title]="isNumber() ? null : universe().name">@for (p of parts(); track $index) {{{ p }}<wbr />}</span>
      @if (where()) { <span class="where mono" [attr.title]="where()">{{ where() }}</span> }
      @if (universe().note) { <span class="note">{{ universe().note }}</span> }
      <span class="foot">
      @if (metrics(); as m) {
        <span class="load">
          @if (state() !== 'stopped') {
            <span class="line">
              <span class="cell">
                <span class="k">CPU</span>
                <mv-sparkline [values]="m.cpuSeries" [max]="m.cpuCap" />
                <span class="v">{{ cores(m.cpu) }}<span class="u">/{{ cores(m.cpuCap) }}</span></span>
              </span>
              <span class="cell">
                <span class="k">RAM</span>
                <mv-sparkline [values]="m.memSeries" [max]="m.memCap || 0" />
                <span class="v" [class.warn]="ramWarn(m)">{{ bytes(m.mem) }}<span class="u">{{ m.memCap ? ' / ' + bytes(m.memCap) : '' }}</span></span>
              </span>
            </span>
            <span class="line sub">
              <span class="cell"><span class="k">Net</span><span class="v">↓ {{ bytes(m.rx) }} ↑ {{ bytes(m.tx) }}</span></span>
              @if (m.data) { <span class="cell"><span class="k">Data</span><span class="v">{{ bytes(m.data) }}</span></span> }
              @if (m.src) { <span class="cell"><span class="k">Src</span><span class="v">{{ bytes(m.src) }}</span></span> }
            </span>
          } @else if (m.data || m.src) {
            <span class="line sub">
              @if (m.data) { <span class="cell"><span class="k">Data</span><span class="v">{{ bytes(m.data) }}</span></span> }
              @if (m.src) { <span class="cell"><span class="k">Src</span><span class="v">{{ bytes(m.src) }}</span></span> }
            </span>
          }
        </span>
      }
      <span class="run" [class]="tone()">
        <span class="dot" [class]="tone()" [class.pulse]="state() === 'partial'"></span>
        <span>{{ status() }}</span>
        @if (behind(); as n) {
          <span class="behind" [attr.title]="n + (n === 1 ? ' commit behind the branch it follows' : ' commits behind the branch it follows')">
            <mv-icon name="branch" /><span>&#x2193;{{ n }}</span>
          </span>
        }
      </span>
      </span>
    </a>`,
  styles: `
    :host { display: block; min-width: 0 }
    .tile { position: relative; display: flex; flex-direction: column; gap: 6px; height: 100%; min-height: 128px; padding: 14px 16px 14px; color: var(--text); text-decoration: none;
      background: var(--panel); border: 1px solid var(--line); border-radius: var(--r-xl); transition: border-color .12s, transform .12s }
    .tile:hover { border-color: var(--accent); transform: translateY(-1px) }
    .tile.stopped { background: transparent }
    .foot { margin-top: auto; display: flex; flex-direction: column; gap: 8px }
    .run { display: flex; align-items: center; gap: 8px; font-size: var(--fs-sm); color: var(--muted) }
    .run.ok { color: var(--ok) } .run.warn { color: var(--warn) }
    .dot { width: 10px; height: 10px; flex: none }
    .wtrow { display: flex; justify-content: flex-end; gap: 8px; margin-bottom: -4px }
    .wt { font-size: var(--fs-2xs); font-weight: 500; color: var(--faint); white-space: nowrap }
    .wt.dirty { color: var(--warn) }
    .num { min-width: 0; font: 300 var(--fs-hero)/1 var(--sans); letter-spacing: -.045em; font-variant-numeric: tabular-nums; overflow-wrap: break-word }
    .num.word { font: 600 var(--fs-xl)/1.2 var(--sans); letter-spacing: -.015em; min-height: 2.4em; display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden }
    .behind { margin-left: auto; display: inline-flex; align-items: center; gap: 3px; color: var(--warn);
      font: 600 var(--fs-2xs)/1 var(--sans); font-variant-numeric: tabular-nums }
    .behind mv-icon { font-size: 1.15em }
    .where { color: var(--muted); font-size: var(--fs-xs); overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    .note { color: var(--muted); font-size: var(--fs-sm); overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    .load { display: flex; flex-direction: column; gap: 5px; padding-top: 9px; border-top: 1px solid var(--line-soft) }
    .line { display: flex; flex-wrap: wrap; gap: 4px 12px; align-items: center }
    .cell { display: inline-flex; align-items: center; gap: 6px; min-width: 0 }
    .k { font: 500 var(--fs-3xs)/1 var(--sans); text-transform: uppercase; letter-spacing: .07em; color: var(--faint) }
    .v { white-space: nowrap; font: 500 var(--fs-2xs)/1 var(--mono); font-variant-numeric: tabular-nums; color: var(--text) }
    .v.warn { color: var(--warn) }
    .v .u { color: var(--faint); font-weight: 400 }
    .load mv-sparkline { display: inline-block; width: 42px; height: 16px }
    @media (prefers-reduced-motion: reduce) { .tile { transition: none } .tile:hover { transform: none } }`,
})
export class UniverseCard {
  private readonly events = inject(Events);
  readonly universe = input.required<UniverseView>();

  protected readonly bytes = bytes;
  /** This universe's live reading, by the same key the server sends it under. */
  protected readonly metrics = computed<PulseUniverse | null>(
    () => this.events.pulse()[`${this.universe().multiverse}/${this.universe().name}`] ?? null,
  );
  protected cores(n: number): string { return n >= 10 ? n.toFixed(0) : n.toFixed(1); }
  protected ramWarn(m: PulseUniverse): boolean { return !!m.memCap && m.mem / m.memCap > 0.85; }

  /** The name in pieces it may break between: after a slash or a dash, not in the middle of a word. */
  protected readonly parts = computed(() => this.universe().name.split(/(?<=[/_-])/));
  protected readonly link = computed(() => universeLink(this.universe().multiverse, this.universe().name));
  protected readonly state = computed(() => runState(this.universe()));
  protected readonly tone = computed(() => ({ running: 'ok', partial: 'warn', stopped: '' })[this.state()]);
  /** Every number is a numeral; a longer name is set smaller, as the word it is. */
  protected readonly isNumber = computed(() => /^\d{1,3}$/.test(this.universe().name));
  protected readonly status = computed(() => {
    const u = this.universe();
    const total = expectedOf(u);
    return this.state() === 'stopped' ? 'Stopped' : this.state() === 'partial' ? `${u.running} of ${total} up` : `Running${total ? ` · ${u.running}/${total}` : ''}`;
  });
  /** Commits the checkouts are behind what they follow; `sync` brings them forward. */
  protected readonly behind = computed(() => this.universe().behind ?? 0);
  /** The checkouts with uncommitted work, and whether there are any. */
  protected readonly dirty = computed(() => dirtyCheckouts(this.universe()));
  protected readonly edited = computed(() => this.dirty().length > 0);
  /** The branch it is on: one name when every repository is on the same. */
  protected readonly where = computed(() => {
    const names = [...new Set(Object.values(this.universe().branches ?? {}).filter(Boolean))];
    // The name already says it when it is the branch: do not say it twice.
    if (names.length === 1) return names[0] === this.universe().name ? '' : names[0];
    return names.length ? `${names.length} branches` : '';
  });
}
