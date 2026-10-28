import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Ops } from '../../core/ops.service';
import { ago, isHttpUrl } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { Icon } from '../../shared/ui/icon';
import { PrChip } from '../../shared/ui/pr-chip';
import { RepoChip } from '../../shared/ui/repo-chip';
import { UniverseStore } from './universe.store';

/**
 * The summary beside every section of a universe: where to reach it, the pull
 * requests open for it, how each repository stands, what was done to it lately,
 * and where it lives. It is what is wanted at a glance, so it stays in view while
 * the section in the middle changes.
 */
@Component({
  selector: 'mv-universe-aside',
  imports: [Badge, CopyButton, Icon, PrChip, RepoChip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './universe-aside.html',
  styleUrl: './universe-aside.css',
})
export class UniverseAside {
  protected readonly store = inject(UniverseStore);
  protected readonly ops = inject(Ops);
  protected readonly u = this.store.universe;
  protected readonly ago = ago;

  protected readonly links = this.store.links;
  protected readonly reviews = this.store.reviews;

  protected readonly repos = computed(() => (this.u()?.checkouts ?? []).map((c) => ({ c, loc: this.store.places().get(c.repo) })));

  /** What was done to this universe in this session, newest first. */
  protected readonly activity = computed(() => this.ops.list()
    .filter((o) => o.multiverse === this.store.multiverse() && o.universe === this.store.name())
    .slice(-5).reverse());

  protected readonly tone = (s: string): string => (s === 'done' ? 'ok' : s === 'failed' ? 'bad' : 'info');

  protected showOutput(id: string): void { this.ops.show(id); this.ops.open.set(true); }
}
