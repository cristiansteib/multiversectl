import { DestroyRef, Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { CodeRow, CodeView, LocView, MultiverseView } from '../../core/models';
import { Ops } from '../../core/ops.service';

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

  /** Its repositories as the Code tab reads them, by name, as of the last fetch: the branch and the review open for it. */
  readonly code = signal<ReadonlyMap<string, CodeRow>>(new Map());
  /** Where each repository lives, by name, for a chip to say it and link to it. */
  readonly places = signal<ReadonlyMap<string, LocView>>(new Map());

  constructor() {
    effect(() => {
      this.query();
      untracked(() => { void this.readCode(); void this.readPlaces(); });
    });
    // What an operation did (a start, an update of the code) is on the page when it ends.
    this.ops.finished$.pipe(takeUntilDestroyed(inject(DestroyRef))).subscribe(() => void this.readCode());
  }

  async readCode(): Promise<void> {
    const q = this.query();
    if (!q.multiverse || !q.universe) return;
    try {
      const view = await this.api.get<CodeView>('universe/code', { ...q, fetch: false });
      this.code.set(new Map(view.rows.map((r) => [r.repo, r])));
    } catch {
      // The chips and the summary are extras: the page is whole without them.
    }
  }

  private async readPlaces(): Promise<void> {
    const name = this.multiverse();
    if (!name) return;
    try {
      const view = await this.api.get<MultiverseView>(`multiverses/${encodeURIComponent(name)}`);
      this.places.set(new Map(view.repos.filter((r) => r.loc).map((r) => [r.key, r.loc as LocView])));
    } catch {
      // Without it a chip names the repository by its key and does not link.
    }
  }
}
