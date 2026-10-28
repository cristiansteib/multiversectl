import { ChangeDetectionStrategy, Component, ElementRef, afterNextRender, input, output, viewChild } from '@angular/core';

/**
 * A dialog on the platform's own <dialog>: focus is trapped, Escape closes it,
 * and what is behind it is inert. The parent shows it by rendering it and hides
 * it by removing it when `closed` fires.
 */
@Component({
  selector: 'mv-modal',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <dialog #dlg class="modal" [attr.aria-label]="title()" (close)="closed.emit()" (click)="backdrop($event)">
      <div class="modal-body">
        <h2>{{ title() }}</h2>
        <ng-content />
      </div>
    </dialog>`,
  styles: `
    dialog { border: 1px solid var(--line); border-radius: var(--r-lg); background: var(--panel); color: var(--text); padding: 0;
      width: min(540px, calc(100vw - 32px)); max-height: calc(100vh - 48px) }
    dialog::backdrop { background: rgb(8 12 16 / .6) }
    .modal-body { padding: 22px 24px 20px } h2 { margin: 0 0 14px; font-size: 1.15rem; letter-spacing: -.01em }`,
})
export class Modal {
  readonly title = input.required<string>();
  readonly closed = output<void>();
  private readonly dlg = viewChild.required<ElementRef<HTMLDialogElement>>('dlg');

  constructor() { afterNextRender(() => this.dlg().nativeElement.showModal()); }

  close(): void { this.dlg().nativeElement.close(); }
  /** A press on the backdrop, not on the dialog's own content. */
  protected backdrop(e: MouseEvent): void { if (e.target === this.dlg().nativeElement) this.close(); }
}
