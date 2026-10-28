import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { CodeRow } from '../../core/models';
import { ago } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { Icon } from '../../shared/ui/icon';
import { isWebUrl } from './patch';

const STATES: Record<string, { icon: string; label: string; word: string }> = {
  open: { icon: 'prOpen', label: 'open', word: 'Open' },
  draft: { icon: 'prDraft', label: 'draft', word: 'Draft' },
  merged: { icon: 'prMerged', label: 'merged', word: 'Merged' },
  closed: { icon: 'prClosed', label: 'closed', word: 'Closed' },
};

/**
 * The pull or merge request of a branch. No chip is the answer when it never
 * had one: a line saying so on every repository of every universe would drown
 * the ones that have one. A service that could not be asked is said, since a
 * branch with no review is not the same as one nobody could ask about.
 */
@Component({
  selector: 'mv-review-chip',
  imports: [Icon, Badge],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (link(); as v) {
      <a class="pr" [class]="v.state || 'open'" [href]="v.url" target="_blank" rel="noopener noreferrer" [attr.title]="title()">
        <mv-icon [name]="state().icon" />
        <b>{{ v.mark }}{{ v.number }}</b>
        <span class="st">{{ state().label }}</span>
        <span class="t">{{ v.title }}</span>
      </a>
    } @else if (row().reviewError) {
      <mv-badge tone="warn" [hint]="'The hosting service could not be asked: ' + row().reviewError">unanswered</mv-badge>
    }`,
  styles: `
    :host { display: inline-flex; min-width: 0; max-width: 100% }
    .pr { display: inline-flex; align-items: center; gap: 6px; min-width: 0; max-width: 100%; padding: 3px 10px; border-radius: 999px; border: 1px solid var(--line);
      background: var(--panel); color: var(--text); text-decoration: none; font-size: .84rem }
    .pr:hover { border-color: var(--accent) }
    .st { color: var(--muted) } .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); min-width: 0 }
    .open mv-icon { color: var(--ok) } .draft mv-icon { color: var(--muted) } .merged mv-icon { color: var(--s7) } .closed mv-icon { color: var(--bad) }`,
})
export class ReviewChip {
  readonly row = input.required<CodeRow>();
  protected readonly link = computed(() => { const v = this.row().review; return v && isWebUrl(v.url) ? v : null; });
  protected readonly state = computed(() => STATES[this.link()?.state ?? 'open'] ?? STATES['open']);
  protected readonly title = computed(() => {
    const v = this.link();
    if (!v) return '';
    return [`${this.state().word} ${v.mark === '!' ? 'merge request' : 'pull request'}`, v.title, v.author && `opened by ${v.author}`, v.updated && `moved ${ago(v.updated)}`].filter(Boolean).join(' · ');
  });
}
