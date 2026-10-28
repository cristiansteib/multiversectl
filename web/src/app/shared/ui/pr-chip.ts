import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { ReviewView } from '../../core/models';
import { ago, isHttpUrl } from '../../core/util';
import { Badge } from './badge';
import { PrIcon } from './pr-icon';

/**
 * A pull or merge request as a chip that opens it in a new tab: its mark in
 * the colour of its state, its number and its title. No chip is the answer when
 * a branch never had one; a service that could not be asked is said, since a
 * branch with no review is not the same as one nobody could ask about.
 */
@Component({
  selector: 'mv-pr-chip',
  imports: [Badge, PrIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (link(); as v) {
      <a class="pr" [class]="v.state || 'open'" [href]="v.url" target="_blank" rel="noopener noreferrer" [attr.title]="title()">
        <mv-pr-icon [state]="v.state" />
        <b>{{ v.mark }}{{ v.number }}</b>
        <span class="st">{{ v.state }}</span>
        @if (!compact()) { <span class="t">{{ v.title }}</span> }
      </a>
    } @else if (error()) {
      <mv-badge tone="warn" [hint]="'The hosting service could not be asked: ' + error()">unanswered</mv-badge>
    }`,
  styles: `
    :host { display: inline-flex; min-width: 0; max-width: 100% }
    .pr { display: inline-flex; align-items: center; gap: 6px; min-width: 0; max-width: 100%; padding: 3px 10px; border-radius: 999px; border: 1px solid var(--line);
      background: var(--panel); color: var(--text); text-decoration: none; font-size: .84rem }
    .pr:hover { border-color: var(--accent) }
    .st { color: var(--muted) } .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); min-width: 0 }`,
})
export class PrChip {
  readonly review = input<ReviewView | null | undefined>();
  /** Why the hosting service could not be asked, when it could not. */
  readonly error = input<string | undefined>();
  /** Number and state only, for a place with little room. */
  readonly compact = input(false);
  protected readonly link = computed(() => { const v = this.review(); return v && isHttpUrl(v.url) ? v : null; });
  protected readonly title = computed(() => {
    const v = this.link();
    if (!v) return '';
    const word = v.state === 'merged' ? 'Merged' : v.state === 'closed' ? 'Closed' : v.state === 'draft' ? 'Draft' : 'Open';
    return [`${word} ${v.mark === '!' ? 'merge request' : 'pull request'}`, v.title, v.author && `opened by ${v.author}`, v.updated && `moved ${ago(v.updated)}`, 'Opens in a new tab'].filter(Boolean).join(' · ');
  });
}
