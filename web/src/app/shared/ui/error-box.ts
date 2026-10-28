import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** What went wrong and, when the server knows it, what to do about it. */
@Component({
  selector: 'mv-error',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div class="error" role="alert">{{ message() }}</div>`,
  styles: `.error { padding: 10px 14px; border: 1px solid var(--bad); border-radius: var(--r-md); background: var(--bad-bg); color: var(--bad); overflow-wrap: anywhere }`,
})
export class ErrorBox {
  readonly message = input.required<string>();
}
