import { ChangeDetectionStrategy, Component, input, signal } from '@angular/core';
import { copyText } from '../../core/util';
import { Icon } from './icon';

/**
 * A value people want in a terminal: one click copies it, and the button says
 * so where it was pressed.
 */
@Component({
  selector: 'mv-copy',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="copy mono" [class.copied]="state() === 'copied'" [class.failed]="state() === 'failed'"
            [attr.title]="'Copy ' + text()" (click)="copy()">
      @switch (state()) {
        @case ('copied') { <mv-icon name="check" /> Copied }
        @case ('failed') { Select it and press ctrl-C }
        @default { <span class="label">{{ label() || text() }}</span><mv-icon name="copy" /> }
      }
    </button>`,
  styles: `
    :host { display: inline-flex; max-width: 100% }
    .copy { display: inline-flex; align-items: center; gap: 8px; max-width: 100%; padding: 3px 8px; border: 1px solid var(--line);
      border-radius: var(--r-sm); background: var(--sunk); color: var(--text); font-size: .9em; cursor: pointer; text-align: left }
    .copy:hover { border-color: var(--accent) }
    .label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap }
    .copied { color: var(--ok); border-color: var(--ok) } .failed { color: var(--warn); border-color: var(--warn) }`,
})
export class CopyButton {
  readonly text = input.required<string>();
  readonly label = input<string>();
  protected readonly state = signal<'idle' | 'copied' | 'failed'>('idle');
  private timer: ReturnType<typeof setTimeout> | undefined;

  protected async copy(): Promise<void> {
    const ok = await copyText(this.text());
    this.state.set(ok ? 'copied' : 'failed');
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.state.set('idle'), 1500);
  }
}
