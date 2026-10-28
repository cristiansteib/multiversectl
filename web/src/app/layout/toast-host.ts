import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Toasts } from '../core/toast.service';

@Component({
  selector: 'mv-toast-host',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="toasts" role="status" aria-live="polite">
      @for (t of toasts.list(); track t.id) {
        <button type="button" class="toast" [class]="t.kind" (click)="toasts.dismiss(t.id)" title="Dismiss">{{ t.text }}</button>
      }
    </div>`,
  styles: `
    .toasts { position: fixed; right: 16px; bottom: 72px; z-index: 40; display: flex; flex-direction: column; gap: 8px; align-items: flex-end; max-width: min(420px, calc(100vw - 32px)) }
    .toast { font: 500 .9rem var(--sans); text-align: left; color: var(--text); background: var(--raise); border: 1px solid var(--line); border-left-width: 3px; border-radius: var(--r-md);
      padding: 10px 14px; box-shadow: 0 6px 24px rgb(0 0 0 / .18); cursor: pointer }
    .toast.ok { border-left-color: var(--ok) } .toast.bad { border-left-color: var(--bad) } .toast.info { border-left-color: var(--info) }`,
})
export class ToastHost {
  protected readonly toasts = inject(Toasts);
}
