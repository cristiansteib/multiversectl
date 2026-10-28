import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { Updater } from '../../core/updater.service';
import { SystemView } from '../../core/models';
import { ago } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { DoctorPanel } from './doctor-panel';
import { ToolsTable } from './tools-table';

/** What this machine runs on: this server, the command line beside it, the tools they drive, and whether the setup is healthy. */
@Component({
  selector: 'mv-system-page',
  imports: [Badge, DoctorPanel, ErrorBox, Icon, Skeleton, ToolsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './system-page.html',
  styleUrl: './system-page.css',
})
export class SystemPage {
  private readonly api = inject(Api);
  protected readonly events = inject(Events);
  protected readonly updater = inject(Updater);

  protected readonly data = signal<SystemView | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly loading = signal(false);
  protected readonly cli = computed(() => this.data()?.tools.find((t) => t.name === 'multiversectl'));
  protected readonly missing = computed(() => (this.data()?.tools ?? []).filter((t) => !t.ok));

  constructor() { void this.load(); }

  protected async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.data.set(await this.api.get<SystemView>('system'));
      this.error.set(null);
    } catch (e) {
      this.error.set((e as Error).message);
    } finally {
      this.loading.set(false);
    }
  }

  /** "3 h" for "3 h ago": for a sentence that already says since. */
  protected since(iso: string | undefined): string { return ago(iso).replace(/ ago$/, ''); }
}
