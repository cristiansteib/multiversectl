import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Api } from '../../core/api.service';
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
  imports: [Badge, CopyButton, ErrorBox, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './details-tab.html',
  styleUrl: './details-tab.css',
})
export class DetailsTab {
  private readonly api = inject(Api);
  protected readonly store = inject(UniverseStore);

  protected readonly u = this.store.universe;
  protected readonly doctor = signal<DoctorView | null>(null);
  protected readonly checking = signal(false);
  protected readonly doctorError = signal<string | null>(null);
  protected readonly branches = computed(() => Object.entries(this.u()?.branches ?? {}).sort((a, b) => a[0].localeCompare(b[0])));
  protected readonly ago = ago;
  protected readonly plural = plural;
  protected readonly tone = (s: string): Tone => CHECK_TONE[s] ?? 'mute';

  protected async check(): Promise<void> {
    this.checking.set(true);
    this.doctorError.set(null);
    try {
      this.doctor.set(await this.api.get<DoctorView>('doctor', this.store.query()));
    } catch (e) {
      this.doctorError.set((e as Error).message);
    } finally {
      this.checking.set(false);
    }
  }
}
