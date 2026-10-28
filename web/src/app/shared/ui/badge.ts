import { ChangeDetectionStrategy, Component, input } from '@angular/core';

export type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'mute';

/** A short state, in a colour and in words: colour is never the only signal. */
@Component({
  selector: 'mv-badge',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<span class="badge" [class]="tone()" [attr.title]="hint() || null"><ng-content /></span>`,
  styles: `:host { display: inline-flex }`,
})
export class Badge {
  readonly tone = input<Tone>('mute');
  readonly hint = input<string>();
}
