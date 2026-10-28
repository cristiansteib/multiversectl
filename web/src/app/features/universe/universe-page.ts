import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { Events } from '../../core/events.service';
import { Ops } from '../../core/ops.service';
import { ago, isHttpUrl } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { EmptyState } from '../../shared/ui/empty-state';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { Drawer } from '../../shared/ui/drawer';
import { PrIcon } from '../../shared/ui/pr-icon';
import { UniverseAside } from './universe-aside';
import { UniverseStore } from './universe.store';

/** One universe: its number, whether it runs, what can be done to it, and its tabs. */
@Component({
  selector: 'mv-universe-page',
  imports: [RouterLink, RouterLinkActive, RouterOutlet, Badge, Icon, EmptyState, Skeleton, UniverseAside, Drawer, PrIcon],
  providers: [UniverseStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './universe-page.html',
  styleUrl: './universe-page.css',
})
export class UniversePage {
  // The route's parameters arrive as inputs.
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();

  protected readonly store = inject(UniverseStore);
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);

  protected readonly u = this.store.universe;
  protected readonly busy = computed(() => this.ops.busy(this.multiverse()));
  protected readonly busyHint = computed(() => (this.busy() ? `Another operation is running in ${this.multiverse()}` : null));
  protected readonly canStart = computed(() => {
    const u = this.u();
    return !!u && (!u.running || u.running < this.store.expected());
  });
  protected readonly branches = computed(() => [...new Set(Object.values(this.u()?.branches ?? {}).filter(Boolean))]);
  protected readonly meta = computed(() => {
    const u = this.u();
    return u ? [u.created && `created ${ago(u.created)}`, u.lastStarted && `started ${ago(u.lastStarted)}`, u.domain].filter(Boolean) : [];
  });
  protected readonly webUrl = isHttpUrl;
  /** The summary opens over the page where it has no column of its own. */
  protected readonly summaryOpen = signal(false);
  /** The section navigation as icons only; kept for the next visit. */
  protected readonly navCollapsed = signal(this.savedCollapse());
  protected readonly repos = computed(() => this.u()?.checkouts.length ?? 0);

  protected toggleNav(): void {
    const next = !this.navCollapsed();
    this.navCollapsed.set(next);
    try { localStorage.setItem('universe-nav-collapsed', next ? '1' : '0'); } catch { /* private mode */ }
  }

  private savedCollapse(): boolean {
    try { return localStorage.getItem('universe-nav-collapsed') === '1'; } catch { return false; }
  }

  protected readonly base = computed(() => ['/universe', this.multiverse(), this.universe()]);

  constructor() {
    effect(() => {
      this.store.multiverse.set(this.multiverse());
      this.store.name.set(this.universe());
    });
  }

  protected act(action: 'up' | 'down' | 'sync'): void {
    void this.ops.runOrSay({ action, multiverse: this.multiverse(), universe: this.universe() });
  }
}
