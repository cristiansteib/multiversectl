import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Badge } from '../../shared/ui/badge';
import { EmptyState } from '../../shared/ui/empty-state';
import { Icon } from '../../shared/ui/icon';
import { reasonsFor } from '../fleet/universe-state';
import { isWebUrl } from './patch';
import { UniverseStore } from './universe.store';

const SERVICE_DOT: Record<string, string> = { running: 'ok', stopped: 'warn' };

/** The first thing about a universe: what needs a look, and what runs in it and where to reach it. */
@Component({
  selector: 'mv-overview-tab',
  imports: [Badge, EmptyState, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview-tab.html',
  styleUrl: './overview-tab.css',
})
export class OverviewTab {
  protected readonly store = inject(UniverseStore);
  protected readonly u = this.store.universe;
  protected readonly reasons = computed(() => { const u = this.u(); return u ? reasonsFor(u) : []; });
  protected readonly webUrl = isWebUrl;
  protected dot = (s: string): string => SERVICE_DOT[s] ?? '';
  protected svcTitle(s: { state: string; detail?: string; published?: string }): string {
    return [s.state, s.detail, s.published && `publishes ${s.published}`].filter(Boolean).join(' · ');
  }
}
