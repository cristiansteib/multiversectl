import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** An empty screen is an invitation to act: say what is missing and what to do. */
@Component({
  selector: 'mv-empty',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="empty">
      <strong>{{ title() }}</strong>
      <p><ng-content /></p>
    </div>`,
  styles: `
    .empty { padding: 28px 20px; border: 1px dashed var(--line); border-radius: var(--r-lg); text-align: center; color: var(--muted) }
    strong { display: block; color: var(--text); margin-bottom: 4px } p { margin: 0 }`,
})
export class EmptyState {
  readonly title = input.required<string>();
}
