import { ChangeDetectionStrategy, Component, inject, input, output, viewChild } from '@angular/core';
import { Ops } from '../../core/ops.service';
import { RemoveUniverseForm } from './remove-universe';
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
      <form (submit)="f.submit($event)">
        <p>This deletes the universe's worktrees, so uncommitted work in them is lost, its containers and its volumes, which hold its data, and its state. The shared clones and their branches are not touched.</p>
        <label class="field">
          <span>Type <b class="mono">{{ universe() }}</b> to confirm</span>
          <input type="text" name="confirm" autocomplete="off" autocapitalize="off" spellcheck="false" autofocus
                 [placeholder]="universe()" [value]="f.typed()" (input)="f.typed.set($any($event.target).value)" />
        </label>
        @if (f.error()) { <mv-error [message]="f.error()!" /> }
        <div class="modal-actions">
          <button type="button" class="btn ghost" (click)="modal.close()">Cancel</button>
          <button type="submit" class="btn danger solid" [disabled]="!f.ready() || f.busy()" [attr.aria-busy]="f.busy()">{{ f.force() ? 'Remove anyway' : 'Remove universe' }}</button>
        </div>
      </form>
    </mv-modal>`,
})
export class RemoveUniverseDialog {
  private readonly modal = viewChild.required(Modal);
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly closed = output<void>();

  protected readonly f = new RemoveUniverseForm(inject(Ops), () => this.multiverse(), () => this.universe(), () => this.modal().close());
}
