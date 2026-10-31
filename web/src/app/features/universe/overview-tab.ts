import { ago, changeSize, isHttpUrl, landedHint } from '../../core/util';
import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { Events } from '../../core/events.service';
import { Toasts } from '../../core/toast.service';
import { Badge } from '../../shared/ui/badge';
import { EmptyState } from '../../shared/ui/empty-state';
import { AppView, Note, UrlView } from '../../core/models';
import { Icon } from '../../shared/ui/icon';
import { PrChip } from '../../shared/ui/pr-chip';
import { RepoChip } from '../../shared/ui/repo-chip';
import { reasonsFor } from '../fleet/universe-state';
import { UniverseStore } from './universe.store';

const SERVICE_DOT: Record<string, string> = { running: 'ok', stopped: 'warn' };

/** One line of an app's table: a service, and the addresses it answers on. */
interface ServiceRow { name: string; state?: string; detail?: string; published?: string; urls: UrlView[] }

/** The rows of an app: its services, each with its addresses; an address for a service that is not listed gets a row of its own. */
function rowsOf(a: AppView): ServiceRow[] {
  const urls = a.urls ?? [];
  const rows: ServiceRow[] = (a.services ?? []).map((s) => ({ ...s, urls: urls.filter((x) => x.service === s.name) }));
  const known = new Set(rows.map((r) => r.name));
  for (const name of [...new Set(urls.filter((x) => !known.has(x.service)).map((x) => x.service))]) {
    rows.push({ name, urls: urls.filter((x) => x.service === name) });
  }
  return rows;
}

/** The first thing about a universe: what needs a look, and what runs in it and where to reach it. */
@Component({
  selector: 'mv-overview-tab',
  imports: [Badge, EmptyState, Icon, PrChip, RepoChip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview-tab.html',
  styleUrl: './overview-tab.css',
})
export class OverviewTab {
  protected readonly store = inject(UniverseStore);
  protected readonly u = this.store.universe;
  protected readonly reasons = computed(() => { const u = this.u(); return u ? reasonsFor(u) : []; });
  /** The apps with the most changes first; the manifest's order settles a tie, and an unread repository counts for nothing. */
  protected readonly apps = computed(() => (this.u()?.apps ?? []).map((a, i) => {
    const row = this.store.code().get(a.repo);
    return { app: a, rows: rowsOf(a), code: row, branch: a.branch || row?.branch, loc: this.store.places().get(a.repo), size: changeSize(row), i };
  }).sort((a, b) => b.size - a.size || a.i - b.i));
  protected readonly webUrl = isHttpUrl;
  protected readonly landedHint = landedHint;

  protected readonly events = inject(Events);
  private readonly toasts = inject(Toasts);
  /** The journal, as the store last read it. */
  protected readonly notes = this.store.notes;
  /** A write is in flight: the form waits rather than queueing a second one. */
  protected readonly saving = signal(false);
  /**
   * The note whose removal has been asked for once, so a stray click loses
   * nothing. It disarms itself, as the restart button on the Services tab
   * does: a row left armed is a row the next click deletes.
   */
  protected readonly removing = signal('');
  private confirmTimer: ReturnType<typeof setTimeout> | undefined;
  protected readonly when = ago;

  constructor() {
    inject(DestroyRef).onDestroy(() => clearTimeout(this.confirmTimer));
  }

  /** Adds what the box says, and empties it only once the note is in. */
  protected async add(e: Event, box: HTMLInputElement): Promise<void> {
    e.preventDefault();
    const text = box.value.trim();
    if (!text || this.saving()) return;
    if (await this.write({ action: 'add', text, by: 'web' })) box.value = '';
  }

  /** First click asks, second removes: a journal is somebody's account. */
  protected async remove(n: Note): Promise<void> {
    if (this.removing() !== n.id) {
      this.removing.set(n.id);
      clearTimeout(this.confirmTimer);
      this.confirmTimer = setTimeout(() => { if (this.removing() === n.id) this.removing.set(''); }, 4000);
      return;
    }
    await this.write({ action: 'rm', id: n.id });
  }

  private async write(body: { action: 'add' | 'rm'; id?: string; text?: string; by?: string }): Promise<boolean> {
    this.saving.set(true);
    try {
      await this.store.writeNote(body);
      return true;
    } catch (err) {
      this.toasts.show((err as Error).message, 'bad');
      // The journal may have moved on without this page — a note removed at a
      // terminal is why a removal here fails — so read it again rather than go
      // on showing what is no longer there.
      await this.store.readNotes();
      return false;
    } finally {
      this.removing.set('');
      this.saving.set(false);
    }
  }

  protected dot = (s: string): string => SERVICE_DOT[s] ?? '';
  protected svcTitle(s: { state?: string; detail?: string; published?: string }): string {
    return [s.state, s.detail, s.published && `publishes ${s.published}`].filter(Boolean).join(' · ');
  }
}
