import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { Events } from '../../core/events.service';
import { Ops } from '../../core/ops.service';
import { NewUniverseDialog } from '../../shared/dialogs/new-universe-dialog';
import { Icon } from '../../shared/ui/icon';

/** The buttons that act on a whole multiverse. Nothing is shown when the server is read-only. */
@Component({
  selector: 'mv-multiverse-actions',
  imports: [Icon, NewUniverseDialog],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!events.readOnly()) {
      <span class="row">
        @if (create()) {
          <button type="button" class="btn small primary" [disabled]="busy()" [attr.title]="tip()" (click)="creating.set(true)">
            <mv-icon name="plus" /> New universe
          </button>
        }
        @if (update()) {
          <button type="button" class="btn small" [disabled]="busy()" [attr.title]="tip() || 'Fetch the manifest and clone what it newly lists'"
                  (click)="ops.runOrSay({ action: 'update', multiverse: name() })">
            <mv-icon name="arrowDown" /> Update
          </button>
        }
      </span>
      @if (creating()) { <mv-new-universe-dialog [multiverse]="name()" (closed)="creating.set(false)" /> }
    }`,
})
export class MultiverseActions {
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  readonly name = input.required<string>();
  readonly create = input(false);
  readonly update = input(false);
  protected readonly creating = signal(false);
  protected readonly busy = computed(() => this.ops.busy(this.name()));
  protected readonly tip = computed(() => (this.busy() ? `Another operation is running in ${this.name()}` : null));
}
