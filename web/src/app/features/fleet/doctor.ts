import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Badge, Tone } from '../../shared/ui/badge';
import { ErrorBox } from '../../shared/ui/error-box';
import { Doctor } from './doctor.service';

const TONE: Record<string, Tone> = { ok: 'ok', warn: 'warn', fail: 'bad' };

/** The button that runs the checks of a multiverse, or of one of its universes. */
@Component({
  selector: 'mv-doctor-button',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="btn small" [disabled]="checking()" [attr.aria-busy]="checking()"
            title="Check that git, docker and the checkouts are in the state this needs" (click)="doctor.run(multiverse(), universe())">
      @if (checking()) { <span class="spin" aria-hidden="true"></span> }
      {{ label() }}
    </button>`,
})
export class DoctorButton {
  protected readonly doctor = inject(Doctor);
  readonly multiverse = input.required<string>();
  readonly universe = input('');
  readonly label = input('Run doctor');
  protected readonly checking = computed(() => this.doctor.results()[this.doctor.key(this.multiverse(), this.universe())]?.state === 'checking');
}

/** What the last run said; it stays until it is dismissed. */
@Component({
  selector: 'mv-doctor-result',
  imports: [Badge, ErrorBox],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (entry(); as e) {
      @switch (e.state) {
        @case ('checking') { <p class="muted" role="status">Checking…</p> }
        @case ('error') { <mv-error [message]="e.error ?? ''" /> }
        @case ('done') {
          <section class="result" aria-label="Doctor result">
            <div class="head">
              <strong>{{ e.report?.ok ? 'No failures' : 'Something failed' }}</strong>
              <span class="grow"></span>
              <button type="button" class="btn small ghost" (click)="doctor.dismiss(multiverse(), universe())">Dismiss</button>
            </div>
            <ul>
              @for (c of e.report?.checks; track c.name) {
                <li><mv-badge [tone]="tone(c.status)">{{ c.status }}</mv-badge><span class="mono">{{ c.name }}</span><span class="muted">{{ c.detail }}</span></li>
              }
            </ul>
          </section>
        }
      }
    }`,
  styles: `
    .result { border: 1px solid var(--line); border-radius: var(--r-md); background: var(--sunk); padding: 10px 14px; margin-top: 10px }
    .head { display: flex; align-items: center; gap: 8px } ul { list-style: none; margin: 6px 0 0; padding: 0 }
    li { display: flex; gap: 10px; align-items: baseline; flex-wrap: wrap; padding: 4px 0 } p { margin: 10px 0 0 }`,
})
export class DoctorResult {
  protected readonly doctor = inject(Doctor);
  readonly multiverse = input.required<string>();
  readonly universe = input('');
  protected readonly entry = computed(() => this.doctor.results()[this.doctor.key(this.multiverse(), this.universe())]);
  protected tone = (s: string): Tone => TONE[s] ?? 'mute';
}
