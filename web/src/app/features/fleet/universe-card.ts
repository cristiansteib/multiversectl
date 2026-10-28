import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { UniverseView } from '../../core/models';
import { universeLink } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { dirtyCheckouts, expectedOf, runState } from './universe-state';

/**
 * One universe as a small tile, and a link to its page: the number people say
 * out loud ("is 14 up?"), whether it is, and only what needs a look. Everything
 * else is one click away, on the universe's own page.
 */
@Component({
  selector: 'mv-universe-card',
  imports: [RouterLink, Badge],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <a class="tile" [class.stopped]="state() === 'stopped'" [routerLink]="link()">
      <span class="top">
        <span class="dot" [class]="tone()" [class.pulse]="state() === 'partial'"></span>
        <span class="num" [class.word]="!isNumber()">{{ universe().name }}</span>
      </span>
      <span class="status" [class]="tone()">{{ status() }}</span>
      @if (flags().length) {
        <span class="flags">@for (f of flags(); track f) { <mv-badge tone="warn">{{ f }}</mv-badge> }</span>
      }
      @if (where()) { <span class="where mono" [attr.title]="where()">{{ where() }}</span> }
      @if (universe().note) { <span class="note">{{ universe().note }}</span> }
    </a>`,
  styles: `
    :host { display: block; min-width: 0 }
    .tile { display: flex; flex-direction: column; gap: 6px; height: 100%; min-height: 128px; padding: 14px 16px 14px; color: var(--text); text-decoration: none;
      background: var(--panel); border: 1px solid var(--line); border-radius: var(--r-xl); transition: border-color .12s, transform .12s }
    .tile:hover { border-color: var(--accent); transform: translateY(-1px) }
    .tile.stopped { background: transparent }
    .top { display: flex; align-items: center; gap: 10px; min-width: 0 }
    .dot { width: 10px; height: 10px; flex: none }
    .num { font: 300 2.6rem/1 var(--sans); letter-spacing: -.045em; font-variant-numeric: tabular-nums; overflow-wrap: anywhere }
    .num.word { font: 600 1.25rem/1.2 var(--sans); letter-spacing: -.015em }
    .status { font-size: .88rem; color: var(--muted) } .status.ok { color: var(--ok) } .status.warn { color: var(--warn) }
    .flags { display: flex; gap: 6px; flex-wrap: wrap }
    .where { color: var(--muted); font-size: .8rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    .note { color: var(--muted); font-size: .86rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    @media (prefers-reduced-motion: reduce) { .tile { transition: none } .tile:hover { transform: none } }`,
})
export class UniverseCard {
  readonly universe = input.required<UniverseView>();

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
  protected readonly flags = computed(() => {
    const u = this.universe();
    return [u.behind ? `${u.behind} behind` : '', dirtyCheckouts(u).length ? 'edited' : ''].filter(Boolean);
  });
  /** The branch it is on: one name when every repository is on the same. */
  protected readonly where = computed(() => {
    const names = [...new Set(Object.values(this.universe().branches ?? {}).filter(Boolean))];
    return names.length === 1 ? names[0] : names.length ? `${names.length} branches` : '';
  });
}
