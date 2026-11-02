import { computed, signal } from '@angular/core';
import { ApiError } from '../../core/api.service';
import { Ops } from '../../core/ops.service';

/**
 * What asking to remove a universe is, whatever surface asks: the name typed,
 * and a second, deliberate press when the server refuses over uncommitted work.
 * The dialog on the Details tab and the popover beside Stop differ in looks only.
 */
export class RemoveUniverseForm {
  readonly typed = signal('');
  readonly error = signal<string | null>(null);
  readonly force = signal(false);
  readonly busy = signal(false);
  readonly ready = computed(() => this.typed() === this.universe());

  constructor(
    private readonly ops: Ops,
    private readonly multiverse: () => string,
    private readonly universe: () => string,
    private readonly done: () => void,
  ) {}

  reset(): void {
    this.typed.set('');
    this.error.set(null);
    this.force.set(false);
  }

  async submit(e: Event): Promise<void> {
    e.preventDefault();
    if (!this.ready() || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      await this.ops.run({ action: 'rm', multiverse: this.multiverse(), universe: this.universe(), confirm: this.typed(), force: this.force() });
      this.done();
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
