import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { Ops } from '../../core/ops.service';
import { RemoveUniverseDialog } from '../../shared/dialogs/remove-universe-dialog';
import { DoctorView } from '../../core/models';
import { ago, plural } from '../../core/util';
import { Badge, Tone } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { UniverseStore } from './universe.store';

const CHECK_TONE: Record<string, Tone> = { ok: 'ok', warn: 'warn', fail: 'bad' };

/** Where this universe lives and what it is made of, and a health check on demand. */
@Component({
  selector: 'mv-details-tab',
  imports: [Badge, CopyButton, ErrorBox, Icon, RemoveUniverseDialog],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './details-tab.html',
  styleUrl: './details-tab.css',
})
export class DetailsTab {
  private readonly api = inject(Api);
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  protected readonly removing = signal(false);
  protected readonly busy = computed(() => this.ops.busy(this.store.multiverse()));
  protected readonly store = inject(UniverseStore);

  protected readonly u = this.store.universe;
  protected readonly doctor = signal<DoctorView | null>(null);
  protected readonly checking = signal(false);
  protected readonly doctorError = signal<string | null>(null);
  protected readonly branches = computed(() => Object.entries(this.u()?.branches ?? {}).sort((a, b) => a[0].localeCompare(b[0])));
  protected readonly ago = ago;
  protected readonly plural = plural;
  protected readonly tone = (s: string): Tone => CHECK_TONE[s] ?? 'mute';

  constructor() {
    // A report is of one universe: another one's page starts without it.
    effect(() => { this.store.query(); untracked(() => { this.doctor.set(null); this.doctorError.set(null); }); });
  }

  protected async check(): Promise<void> {
    const q = this.store.query();
    this.checking.set(true);
    this.doctorError.set(null);
    try {
      const report = await this.api.get<DoctorView>('doctor', q);
      if (this.store.isCurrent(q)) this.doctor.set(report);
    } catch (e) {
      if (this.store.isCurrent(q)) this.doctorError.set((e as Error).message);
    } finally {
      this.checking.set(false);
    }
  }
}
