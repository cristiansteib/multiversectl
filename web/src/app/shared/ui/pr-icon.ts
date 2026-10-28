import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Icon } from './icon';

export type ReviewState = 'open' | 'draft' | 'merged' | 'closed';

const STATES: Record<ReviewState, { icon: string; word: string }> = {
  open: { icon: 'prOpen', word: 'Open' },
  draft: { icon: 'prDraft', word: 'Draft' },
  merged: { icon: 'prMerged', word: 'Merged' },
  closed: { icon: 'prClosed', word: 'Closed' },
};

/**
 * The mark of a pull or merge request, drawn for its state: the shape says what
 * it is (open, draft, merged, closed) and so does the colour, the way the
 * hosting services draw them. Every place that shows one uses this.
 */
@Component({
  selector: 'mv-pr-icon',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<mv-icon [name]="meta().icon" [class]="state()" [attr.title]="meta().word" />`,
  styles: `
    :host { display: inline-flex }
    .open { color: var(--ok) } .draft { color: var(--muted) } .merged { color: var(--s7) } .closed { color: var(--bad) }`,
})
export class PrIcon {
  readonly state = input<string | undefined>('open');
  protected readonly meta = computed(() => STATES[(this.state() as ReviewState)] ?? STATES.open);
}
