import { Injectable, computed, inject, signal } from '@angular/core';
import { Events } from './events.service';
import { Ops } from './ops.service';
import { Toasts } from './toast.service';

/**
 * Updating multiversectl from the page. The server downloads the release,
 * checks it, replaces its programs and starts again; the page notices the new
 * version when the connection comes back and reloads itself.
 */
@Injectable({ providedIn: 'root' })
export class Updater {
  private readonly events = inject(Events);
  private readonly ops = inject(Ops);
  private readonly toasts = inject(Toasts);

  /** The release that is out and newer than this server, when there is one. */
  readonly notice = this.events.update;
  readonly dialog = signal(false);
  readonly running = computed(() => this.ops.list().some((o) => o.action === 'selfupdate' && o.state === 'running'));

  show(): void { this.dialog.set(true); }
  hide(): void { this.dialog.set(false); }

  async install(): Promise<void> {
    try {
      await this.ops.run({ action: 'selfupdate' });
      this.hide();
    } catch (e) {
      this.toasts.show((e as Error).message, 'bad');
    }
  }
}
