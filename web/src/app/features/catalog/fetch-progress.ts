import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { shortRepoName } from '../../core/util';
import { Badge, Tone } from '../../shared/ui/badge';
import { Step } from './catalog.model';

const PHASE: Record<string, [Tone, string]> = {
  fetching: ['info', 'Reading'],
  done: ['ok', 'Up to date'],
  stale: ['warn', 'Not refreshed'],
  unavailable: ['bad', 'Unavailable'],
};

/**
 * Catalog repositories are read at the same time, so each one shows its own
 * state as it answers instead of one spinner for all of them.
 */
@Component({
  selector: 'mv-fetch-progress',
  imports: [Badge],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="card progress" aria-live="polite">
      <h3>Reading catalogs <span class="muted count">{{ finished() }} of {{ steps().length }}</span></h3>
      <ul class="list">
        @for (s of steps(); track s.repo) {
          <li>
            <span class="repo mono" [attr.title]="s.repo">{{ short(s.repo) }}</span>
            <span class="state">
              @if (s.phase === 'fetching') { <span class="spin" role="status" aria-label="Reading"></span> }
              <mv-badge [tone]="tone(s.phase)" [hint]="s.detail">{{ label(s.phase) }}@if (s.ms) { · {{ seconds(s.ms) }} }</mv-badge>
            </span>
            @if (s.detail && s.phase !== 'fetching') { <span class="detail muted">{{ s.detail }}</span> }
          </li>
        }
      </ul>
    </section>`,
  styles: `
    .progress { margin-bottom: 16px }
    .count { font-weight: 400; margin-left: 6px }
    li { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 2px 14px; align-items: center }
    .repo { overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    .state { display: inline-flex; align-items: center; gap: 8px; justify-content: flex-end }
    .detail { grid-column: 1 / -1; font-size: .88rem; overflow-wrap: anywhere }`,
})
export class FetchProgress {
  readonly map = input.required<ReadonlyMap<string, Step>>();
  protected readonly steps = computed(() => [...this.map().values()]);
  protected readonly finished = computed(() => this.steps().filter((s) => s.phase !== 'fetching').length);
  protected readonly short = shortRepoName;
  protected tone = (p: string): Tone => PHASE[p]?.[0] ?? 'mute';
  protected label = (p: string): string => PHASE[p]?.[1] ?? p;
  protected seconds = (ms: number): string => (ms / 1000).toFixed(1) + 's';
}
