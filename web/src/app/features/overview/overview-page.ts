import { Op } from '../../core/models';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Events } from '../../core/events.service';
import { Ops } from '../../core/ops.service';
import { Toasts } from '../../core/toast.service';
import { plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Place } from '../../shared/ui/place';
import { Skeleton } from '../../shared/ui/skeleton';
import { MultiverseActions } from '../fleet/multiverse-actions';
import { reasonsFor, runState } from '../fleet/universe-state';

/** One thing a person could do something about, with the way to do it. */
interface Attention {
  id: string;
  tone: 'bad' | 'warn' | 'info';
  title: string;
  detail?: string;
  /** A command to copy, a link to follow, or a button; whichever fixes it fastest. */
  command?: string;
  link?: { label: string; to: string[]; query?: Record<string, string> };
  action?: { label: string; run: () => void; busy?: boolean };
}

/** The machine at a glance: what needs a look first, then what is installed. */
@Component({
  selector: 'mv-overview-page',
  imports: [RouterLink, Badge, CopyButton, EmptyState, ErrorBox, Icon, Place, Skeleton, MultiverseActions],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview-page.html',
  styleUrl: './overview-page.css',
})
export class OverviewPage {
  protected readonly events = inject(Events);
  private readonly ops = inject(Ops);
  private readonly toasts = inject(Toasts);

  protected readonly data = this.events.overview;
  protected readonly status = computed(() => this.data()?.status);
  protected readonly multiverses = computed(() => this.status()?.multiverses ?? []);
  protected readonly fleet = computed(() => this.events.fleet()?.multiverses ?? []);
  protected readonly comparing = signal(false);
  protected plural = plural;

  private readonly universes = computed(() => this.fleet().flatMap((w) => w.universes));
  protected readonly running = computed(() => this.universes().filter((u) => u.running).length);
  protected readonly stopped = computed(() => this.universes().filter((u) => runState(u) === 'stopped').length);

  /** How many universes and how many running, for one multiverse's row. */
  protected countsOf(name: string): { total: number; running: number } {
    const list = this.fleet().find((w) => w.name === name)?.universes ?? [];
    return { total: list.length, running: list.filter((u) => u.running).length };
  }

  protected readonly summary = computed(() => {
    const w = this.multiverses().length;
    if (!w) return 'Nothing is installed on this machine yet.';
    const total = this.universes().length;
    return `${plural(w, 'multiverse')} installed, ${plural(total, 'universe')}, ${this.running()} running.`;
  });

  protected readonly attention = computed<Attention[]>(() => {
    const out: Attention[] = [];
    const st = this.status();
    if (!st) return out;

    if (st.catalog.unavailable) {
      out.push({ id: 'catalog', tone: 'bad', title: 'The catalog cannot be read', detail: st.catalog.unavailable,
        action: { label: 'Try again', run: () => void this.compare() } });
    } else if (st.catalog.noRemote) {
      out.push({ id: 'catalog-local', tone: 'warn', title: 'The catalog exists only on this machine',
        detail: 'It is lost with this machine, and nobody else can use it.', command: 'multiversectl catalog push' });
    }

    for (const w of st.multiverses ?? []) {
      if (w.stale || w.manifestDiverged || w.manifestEdited || w.advice) {
        out.push({ id: 'drift-' + w.name, tone: w.stale ? 'warn' : 'info', title: `${w.name} has drifted from where it came from`,
          detail: w.advice || w.notes?.join(', '),
          action: w.stale && !this.events.readOnly() ? { label: 'Update', busy: this.ops.busy(w.name), run: () => void this.ops.runOrSay({ action: 'update', multiverse: w.name }) } : undefined,
          link: { label: 'Open', to: ['/multiverse', w.name] } });
      }
    }

    for (const w of this.fleet()) {
      for (const u of w.universes) {
        for (const reason of reasonsFor(u)) {
          const behind = reason.includes('behind');
          out.push({ id: `${w.name}/${u.name}/${reason}`, tone: 'warn', title: `Universe ${u.name} in ${w.name}`, detail: reason,
            action: behind && !this.events.readOnly() ? { label: 'Update code', busy: this.ops.busy(w.name), run: () => void this.ops.runOrSay({ action: 'sync', multiverse: w.name, universe: u.name }) } : undefined,
            link: { label: 'Open', to: ['/universe', w.name, u.name] } });
        }
      }
    }

    // A failure is news until the same thing has been tried again: the latest try
    // of each action on each universe is the one that counts.
    const latest = new Map<string, Op>();
    for (const op of this.ops.list()) latest.set(`${op.multiverse}/${op.universe ?? ''}/${op.action}`, op);
    for (const op of latest.values()) {
      if (op.state === 'failed') {
        out.push({ id: 'op-' + op.id, tone: 'bad', title: `${op.title} failed`, detail: op.error,
          action: { label: 'Show output', run: () => { this.ops.show(op.id); this.ops.open.set(true); } } });
      }
    }
    return out;
  });

  protected async compare(): Promise<void> {
    this.comparing.set(true);
    try {
      const ok = await this.events.reload(true);
      this.toasts.show(ok ? 'Compared with the remotes' : (this.events.error() ?? 'Could not compare with the remotes'), ok ? 'ok' : 'bad');
    } finally {
      this.comparing.set(false);
    }
  }
}
