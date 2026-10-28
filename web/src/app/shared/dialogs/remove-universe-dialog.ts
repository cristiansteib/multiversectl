import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal, viewChild } from '@angular/core';
import { ApiError } from '../../core/api.service';
import { Ops } from '../../core/ops.service';
import { ErrorBox } from '../ui/error-box';
import { Modal } from '../ui/modal';

/**
 * Removing a universe deletes its worktrees, containers and volumes, so the
 * universe's own name has to be typed. Uncommitted work is never discarded by
 * accident: the server refuses and says where it is, and only a second,
 * deliberate press removes it.
 */
@Component({
  selector: 'mv-remove-universe-dialog',
  imports: [Modal, ErrorBox],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mv-modal #modal [title]="'Remove universe ' + universe() + '?'" (closed)="closed.emit()">
      <form (submit)="submit($event)">
        <p>This deletes the universe's worktrees, so uncommitted work in them is lost, its containers and its volumes, which hold its data, and its state. The shared clones and their branches are not touched.</p>
        <label class="field">
          <span>Type <b class="mono">{{ universe() }}</b> to confirm</span>
          <input type="text" name="confirm" autocomplete="off" autocapitalize="off" spellcheck="false" autofocus
                 [placeholder]="universe()" [value]="typed()" (input)="typed.set($any($event.target).value)" />
        </label>
        @if (error()) { <mv-error [message]="error()!" /> }
        <div class="modal-actions">
          <button type="button" class="btn ghost" (click)="modal.close()">Cancel</button>
          <button type="submit" class="btn danger solid" [disabled]="!ready() || busy()" [attr.aria-busy]="busy()">{{ force() ? 'Remove anyway' : 'Remove universe' }}</button>
        </div>
      </form>
    </mv-modal>`,
})
export class RemoveUniverseDialog {
  private readonly ops = inject(Ops);
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly closed = output<void>();

  private readonly modal = viewChild.required(Modal);
  protected readonly typed = signal('');
  protected readonly error = signal<string | null>(null);
  protected readonly force = signal(false);
  protected readonly busy = signal(false);
  protected readonly ready = computed(() => this.typed() === this.universe());

  protected async submit(e: Event): Promise<void> {
    e.preventDefault();
    if (!this.ready() || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      await this.ops.run({ action: 'rm', multiverse: this.multiverse(), universe: this.universe(), confirm: this.typed(), force: this.force() });
      this.modal().close();
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && err.body['dirty']) {
        this.force.set(true);
        this.error.set(`${err.message}. Press the button again to discard it.`);
      } else {
        this.error.set((err as Error).message);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
