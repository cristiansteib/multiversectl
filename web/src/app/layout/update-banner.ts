import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Events } from '../core/events.service';
import { CopyButton } from '../shared/ui/copy-button';

/**
 * A newer release than this server, and what it fixes, above everything: it is
 * the one thing on the page that is about the tool and not about the fleet.
 */
@Component({
  selector: 'mv-update-banner',
  imports: [CopyButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (events.update(); as n) {
      <div class="update" role="status">
        <div class="line">
          <strong>Update available</strong>
          <span>multiversectl {{ n.latest }} is out; you are on {{ n.current }}@if (brings()) {, {{ brings() }} since}.</span>
          <mv-copy text="multiversectl self-update" />
          <a [href]="n.url" target="_blank" rel="noopener noreferrer">Release notes</a>
        </div>
        @if (n.highlights?.length) {
          <details>
            <summary>What's fixed</summary>
            <ul>
              @for (f of n.highlights; track f) { <li>{{ f }}</li> }
              @if (rest(); as more) { <li class="muted">{{ more }}</li> }
            </ul>
          </details>
        }
      </div>
    }`,
  styles: `
    .update { padding: 10px 20px; background: var(--info-bg); border-bottom: 1px solid var(--info) }
    .line { display: flex; align-items: center; gap: 12px; flex-wrap: wrap }
    details { margin-top: 6px } summary { cursor: pointer; color: var(--muted) } ul { margin: 6px 0 0; padding-left: 20px }`,
})
export class UpdateBanner {
  protected readonly events = inject(Events);
  protected readonly brings = computed(() => {
    const n = this.events.update();
    if (!n) return '';
    const count = (k: number, one: string, many: string) => `${k}${n.more ? '+' : ''} ${k === 1 && !n.more ? one : many}`;
    return n.fixes ? count(n.fixes, 'fix', 'fixes') : n.changes ? count(n.changes, 'change', 'changes') : '';
  });
  protected readonly rest = computed(() => {
    const n = this.events.update();
    if (!n) return null;
    const more = n.fixes - (n.highlights?.length ?? 0);
    return more > 0 ? `and ${more}${n.more ? '+' : ''} more` : n.more ? 'and older ones' : null;
  });
}
