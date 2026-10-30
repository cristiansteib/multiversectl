import { DestroyRef, Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { CodeRow, CodeView, LocView, MultiverseView, Note } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { isHttpUrl } from '../../core/util';

/**
 * Which universe this page is about, and what is known of it. It is provided by
 * the page, so every tab and the summary beside them read the same one; none of
 * them reads the router for it.
 */
@Injectable()
export class UniverseStore {
  private readonly events = inject(Events);
  private readonly api = inject(Api);
  private readonly ops = inject(Ops);

  readonly multiverse = signal('');
  readonly name = signal('');

  /** False until the fleet has been read once: an absent universe is only "gone" after that. */
  readonly loaded = computed(() => this.events.fleet() !== null);
  private readonly mv = computed(() => this.events.fleet()?.multiverses.find((m) => m.name === this.multiverse()));
  readonly universe = computed(() => this.mv()?.universes.find((u) => u.name === this.name()));
  readonly expected = computed(() => this.universe()?.apps.reduce((n, a) => n + a.expected, 0) ?? 0);
  readonly query = computed(() => ({ multiverse: this.multiverse(), universe: this.name() }));

  /** Every web address of its apps, once each, with the service that answers on it. */
  readonly links = computed(() => {
    const seen = new Set<string>();
    const out: { url: string; label: string; from: string }[] = [];
    for (const a of this.universe()?.apps ?? []) {
      for (const x of a.urls ?? []) {
        if (!isHttpUrl(x.url) || seen.has(x.url)) continue;
        seen.add(x.url);
        out.push({ url: x.url, label: x.url.replace(/^https?:\/\//, ''), from: a.key === x.service ? a.key : `${a.key}/${x.service}` });
      }
    }
    return out;
  });

  /** Its repositories as the Code tab reads them, by name, as of the last fetch: the branch and the review open for it. */
  readonly code = signal<ReadonlyMap<string, CodeRow>>(new Map());
  /** The repositories that have a pull or merge request, or whose service could not be asked. */
  readonly reviews = computed(() => [...this.code().values()].filter((r) => r.review || r.reviewError));

  /** Where each repository lives, by name, for a chip to say it and link to it. */
  readonly places = signal<ReadonlyMap<string, LocView>>(new Map());

  /**
   * Its journal, oldest first: what has been done in the universe, by whoever
   * said so. It is read when the page opens and after a write; nothing else
   * changes it, so it is not in the fleet every page polls.
   */
  readonly notes = signal<readonly Note[]>([]);

  /** Whether an answer is still about the universe in view: the same name in another multiverse is another universe. */
  isCurrent(q: { multiverse: string; universe: string }): boolean {
    return q.multiverse === this.multiverse() && q.universe === this.name();
  }

  constructor() {
    effect(() => {
      this.query();
      untracked(() => {
        // What was read for the universe before is not this one's.
        this.code.set(new Map());
        this.places.set(new Map());
        this.notes.set([]);
        void this.readCode();
        void this.readPlaces();
        void this.readNotes();
      });
    });
    // What an operation did (a start, an update of the code) is on the page when it ends.
    this.ops.finished$.pipe(takeUntilDestroyed(inject(DestroyRef))).subscribe(() => void this.readCode());
  }

  async readCode(): Promise<void> {
    const q = this.query();
    if (!q.multiverse || !q.universe) return;
    try {
      const view = await this.api.get<CodeView>('universe/code', { ...q, fetch: false });
      if (!this.isCurrent(q)) return;
      this.code.set(new Map(view.rows.map((r) => [r.repo, r])));
    } catch {
      // The chips and the summary are extras: the page is whole without them.
    }
  }

  async readNotes(): Promise<void> {
    const q = this.query();
    if (!q.multiverse || !q.universe) return;
    try {
      const view = await this.api.get<{ notes: Note[] }>('universe/notes', q);
      if (!this.isCurrent(q)) return;
      this.notes.set(view.notes ?? []);
    } catch {
      // A journal nobody can read is still a universe: the page is whole without it.
    }
  }

  /** Adds a line, changes one or takes one out, then reads the journal back. */
  async writeNote(body: { action: 'add' | 'set' | 'rm'; id?: string; text?: string; by?: string }): Promise<void> {
    await this.api.post<Note>('universe/notes', { ...this.query(), ...body });
    await this.readNotes();
  }

  private async readPlaces(): Promise<void> {
    const name = this.multiverse();
    if (!name) return;
    try {
      const view = await this.api.get<MultiverseView>(`multiverses/${encodeURIComponent(name)}`);
      if (name !== this.multiverse()) return;
      this.places.set(new Map(view.repos.filter((r) => r.loc).map((r) => [r.key, r.loc as LocView])));
    } catch {
      // Without it a chip names the repository by its key and does not link.
    }
  }
}
