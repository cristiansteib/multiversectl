import { Injectable, computed, inject, signal } from '@angular/core';
import { Api } from './api.service';
import { Events } from './events.service';
import { UpdateNotice } from './models';
import { Ops } from './ops.service';
import { Toasts } from './toast.service';
import { clock } from './util';

const KEY = 'update-reminder';
const LATER_MS = 24 * 3600 * 1000;
const LOOK_EVERY_MS = 15 * 60 * 1000;

/** What was answered about a release: ask again tomorrow, or not about this one. */
interface Reminder { version: string; mode: 'later' | 'skip'; until?: number }

/**
 * Updating multiversectl from the page. The server downloads the release,
 * checks it, replaces its programs and starts again; the page notices the new
 * version when the connection comes back and reloads itself.
 *
 * A newer release is announced on every page until the person answers: update
 * now, remind me later (tomorrow), or skip this version (until a newer one).
 */
@Injectable({ providedIn: 'root' })
export class Updater {
  private readonly api = inject(Api);
  private readonly events = inject(Events);
  private readonly ops = inject(Ops);
  private readonly toasts = inject(Toasts);

  /** The release that is out and newer than this server, when there is one. */
  readonly notice = this.events.update;
  readonly dialog = signal(false);
  readonly running = computed(() => this.ops.list().some((o) => o.action === 'selfupdate' && o.state === 'running'));

  private readonly reminder = signal<Reminder | null>(this.read());
  /** Hidden by the person's choice for this very version. */
  private readonly answered = computed<'later' | 'skip' | null>(() => {
    const n = this.notice();
    const r = this.reminder();
    if (!n || !r || r.version !== n.latest) return null; // a newer release is news again
    if (r.mode === 'skip') return 'skip';
    return clock() < (r.until ?? 0) ? 'later' : null; // the clock moves, so "tomorrow" arrives by itself
  });
  readonly bannerVisible = computed(() => !!this.notice() && !this.answered());
  /** Put off for a day: the navbar keeps a small mark, so it is not forgotten. */
  readonly snoozed = computed(() => this.answered() === 'later');

  constructor() {
    void this.look();
    setInterval(() => { if (!document.hidden) void this.look(); }, LOOK_EVERY_MS);
  }

  show(): void { this.dialog.set(true); }
  hide(): void { this.dialog.set(false); }

  later(): void { this.remember({ version: this.notice()?.latest ?? '', mode: 'later', until: Date.now() + LATER_MS }); }
  skip(): void { this.remember({ version: this.notice()?.latest ?? '', mode: 'skip' }); }

  /** Update straight away: the dock shows it, and the page reloads when the server is back. */
  async install(): Promise<void> {
    try {
      await this.ops.run({ action: 'selfupdate' });
      this.hide();
    } catch (e) {
      this.toasts.show((e as Error).message, 'bad');
    }
  }

  /** Ask the server whether a release is out, rather than wait for its own look. */
  private async look(): Promise<void> {
    try {
      const v = await this.api.get<{ notice?: UpdateNotice }>('update');
      this.events.update.set(v.notice ?? null);
    } catch {
      // The server's own look tells the page in time.
    }
  }

  private remember(r: Reminder): void {
    if (!r.version) return;
    this.reminder.set(r);
    try { localStorage.setItem(KEY, JSON.stringify(r)); } catch { /* private mode */ }
  }

  private read(): Reminder | null {
    try {
      const r = JSON.parse(localStorage.getItem(KEY) ?? 'null') as Reminder | null;
      return r && typeof r.version === 'string' && (r.mode === 'later' || r.mode === 'skip') ? r : null;
    } catch {
      return null;
    }
  }
}
