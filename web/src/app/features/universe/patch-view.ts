import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { PatchView as Patch } from '../../core/models';
import { ErrorBox } from '../../shared/ui/error-box';
import { Skeleton } from '../../shared/ui/skeleton';
import { classifyPatch } from './patch';

/** One file's diff, a line at a time: what each line is comes from its first character, and none of it is markup. */
@Component({
  selector: 'mv-patch-view',
  imports: [ErrorBox, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!patch()) { <mv-skeleton height="4rem" /> }
    @else if (failure(); as e) { <mv-error [message]="e" /> }
    @else {
      <div class="patch mono" tabindex="0" role="region" aria-label="Diff">
        @for (l of lines(); track $index) { <div class="pl" [class]="l.kind">{{ l.text }}</div> }
        @if (cut()) { <div class="pl meta">… cut: the rest of this diff is not shown here.</div> }
      </div>
    }`,
  styles: `
    .patch { overflow: auto; max-height: 70vh; background: var(--sunk); border-top: 1px solid var(--line-soft); font-size: .8rem; line-height: 1.5; padding: 6px 0 }
    .pl { white-space: pre; padding: 0 14px; width: max-content; min-width: 100% }
    .ins { background: var(--ok-bg); color: var(--ok) } .rem { background: var(--bad-bg); color: var(--bad) }
    .hunk { color: var(--info); background: var(--info-bg) } .meta { color: var(--faint) }`,
})
export class PatchViewer {
  readonly patch = input<Patch | { error: string } | undefined>();
  protected readonly failure = computed(() => { const p = this.patch(); return p && 'error' in p ? p.error : null; });
  private readonly real = computed(() => { const p = this.patch(); return p && !('error' in p) ? p : null; });
  protected readonly lines = computed(() => classifyPatch(this.real()?.patch ?? ''));
  protected readonly cut = computed(() => !!this.real()?.truncated);
}
