import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, afterNextRender, inject, input, output, viewChild } from '@angular/core';
import { Icon } from './icon';

/**
 * A panel that slides in from the right over the page, on the platform's own
 * <dialog>: Escape and a press outside close it, focus stays inside while it is
 * open, and goes back to what opened it. The parent shows it by rendering it and
 * hides it by removing it when `closed` fires.
 */
@Component({
  selector: 'mv-drawer',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <dialog #dlg class="drawer" [attr.aria-label]="title()" (close)="closed.emit()" (click)="backdrop($event)">
      <header>
        <h2>{{ title() }}</h2>
        <button type="button" class="btn small icon ghost" (click)="close()" aria-label="Close"><mv-icon name="x" /></button>
      </header>
      <div class="body"><ng-content /></div>
    </dialog>`,
  styles: `
    dialog { position: fixed; inset: 0 0 0 auto; margin: 0; width: min(400px, 100vw); height: 100dvh; max-height: none; padding: 0; border: 0; border-left: 1px solid var(--line);
      background: var(--panel); color: var(--text); animation: slide .18s ease-out }
    dialog::backdrop { background: rgb(8 12 16 / .5) }
    header { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--line-soft); position: sticky; top: 0; background: var(--panel); z-index: 1 }
    h2 { margin: 0; font-size: 1.05rem }
    .body { padding: 18px }
    @keyframes slide { from { transform: translateX(24px); opacity: 0 } }
    @media (prefers-reduced-motion: reduce) { dialog { animation: none } }`,
})
export class Drawer {
  readonly title = input.required<string>();
  readonly closed = output<void>();
  private readonly dlg = viewChild.required<ElementRef<HTMLDialogElement>>('dlg');

  constructor() {
    const opener = document.activeElement as HTMLElement | null;
    inject(DestroyRef).onDestroy(() => { if (opener?.isConnected) opener.focus(); });
    afterNextRender(() => this.dlg().nativeElement.showModal());
  }

  close(): void { this.dlg().nativeElement.close(); }
  protected backdrop(e: MouseEvent): void { if (e.target === this.dlg().nativeElement) this.close(); }
}
