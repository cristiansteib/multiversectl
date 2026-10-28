import { Injectable, computed, inject, signal } from '@angular/core';
import { Events } from '../../core/events.service';

/**
 * Which universe this page is about, and what the fleet says of it. It is
 * provided by the page, so every tab under it reads the same one; the tabs
 * never read the router for it.
 */
@Injectable()
export class UniverseStore {
  private readonly events = inject(Events);

  readonly multiverse = signal('');
  readonly name = signal('');

  /** False until the fleet has been read once: an absent universe is only "gone" after that. */
  readonly loaded = computed(() => this.events.fleet() !== null);
  private readonly mv = computed(() => this.events.fleet()?.multiverses.find((m) => m.name === this.multiverse()));
  readonly universe = computed(() => this.mv()?.universes.find((u) => u.name === this.name()));
  readonly expected = computed(() => this.universe()?.apps.reduce((n, a) => n + a.expected, 0) ?? 0);
  readonly query = computed(() => ({ multiverse: this.multiverse(), universe: this.name() }));
}
