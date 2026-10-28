import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Events } from '../../core/events.service';
import { UniverseView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { universeLink } from '../../core/util';
import { RemoveUniverseDialog } from '../../shared/dialogs/remove-universe-dialog';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { Badge } from '../../shared/ui/badge';
import { Icon } from '../../shared/ui/icon';
import { DoctorButton, DoctorResult } from './doctor';
import { expectedOf, isWebUrl, reasonsFor, runState } from './universe-state';

const SERVICE_DOT: Record<string, string> = { running: 'ok', stopped: 'warn' };

/**
 * One universe as a tile. Its number is what people say out loud ("is 14 up?"),
 * so it is the largest thing on it; the dot beside it says whether it is.
 */
@Component({
  selector: 'mv-universe-card',
  imports: [RouterLink, Badge, Icon, AgoPipe, DoctorButton, DoctorResult, RemoveUniverseDialog],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './universe-card.html',
  styleUrl: './universe-card.css',
})
export class UniverseCard {
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  readonly universe = input.required<UniverseView>();

  protected readonly link = computed(() => universeLink(this.universe().multiverse, this.universe().name));
  protected readonly state = computed(() => runState(this.universe()));
  protected readonly total = computed(() => expectedOf(this.universe()));
  protected readonly reasons = computed(() => reasonsFor(this.universe()));
  protected readonly busy = computed(() => this.ops.busy(this.universe().multiverse));
  protected readonly tip = computed(() => (this.busy() ? `Another operation is running in ${this.universe().multiverse}` : null));
  protected readonly removing = signal(false);
  /** Every number is a numeral; a longer name is set smaller, as the word it is. */
  protected readonly isNumber = computed(() => /^\d{1,3}$/.test(this.universe().name));
  protected readonly branchNames = computed(() => [...new Set(Object.values(this.universe().branches ?? {}).filter(Boolean))]);
  protected readonly meta = computed(() => {
    const u = this.universe();
    return [u.domain].filter(Boolean);
  });

  protected dot = (s: string): string => SERVICE_DOT[s] ?? '';
  protected isWeb = isWebUrl;
  protected svcTitle(s: { state: string; detail?: string; published?: string }): string {
    return [s.state, s.detail, s.published && `publishes ${s.published}`].filter(Boolean).join(' · ');
  }

  protected run(action: 'up' | 'down' | 'sync'): void {
    const u = this.universe();
    void this.ops.runOrSay({ action, multiverse: u.multiverse, universe: u.name });
  }
}
