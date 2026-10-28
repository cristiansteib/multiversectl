import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { DoctorView } from '../../core/models';
import { Badge, Tone } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';

const TONE: Record<string, Tone> = { ok: 'ok', warn: 'warn', fail: 'bad' };
const WORD: Record<string, string> = { ok: 'OK', warn: 'Warning', fail: 'Failed' };

/**
 * Checks one multiverse, or one of its universes, for what stops it from
 * running: git, the container runtime, the proxy, free ports. The checks are
 * per multiverse because what they look at lives in its manifest.
 */
@Component({
  selector: 'mv-doctor-panel',
  imports: [Badge, CopyButton, EmptyState, ErrorBox, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './doctor-panel.html',
  styleUrl: './doctor-panel.css',
})
export class DoctorPanel {
  private readonly api = inject(Api);
  private readonly events = inject(Events);

  protected readonly multiverses = computed(() => {
    const names = new Set<string>();
    for (const m of this.events.overview()?.status.multiverses ?? []) names.add(m.name);
    for (const m of this.events.fleet()?.multiverses ?? []) names.add(m.name);
    return [...names].sort();
  });
  protected readonly chosen = signal('');
  protected readonly multiverse = computed(() => (this.multiverses().includes(this.chosen()) ? this.chosen() : (this.multiverses()[0] ?? '')));
  protected readonly universes = computed(() => this.events.fleet()?.multiverses.find((m) => m.name === this.multiverse())?.universes ?? []);
  protected readonly universe = signal('');
  protected readonly running = signal(false);
  protected readonly report = signal<{ of: string; view: DoctorView } | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly onlyProblems = signal(false);

  protected readonly counts = computed(() => {
    const checks = this.report()?.view.checks ?? [];
    return { ok: checks.filter((c) => c.status === 'ok').length, warn: checks.filter((c) => c.status === 'warn').length, fail: checks.filter((c) => c.status === 'fail').length };
  });
  protected readonly shown = computed(() => {
    const checks = this.report()?.view.checks ?? [];
    return this.onlyProblems() ? checks.filter((c) => c.status !== 'ok') : checks;
  });

  protected readonly tone = (s: string): Tone => TONE[s] ?? 'mute';
  protected readonly word = (s: string): string => WORD[s] ?? s;

  protected pick(name: string): void { this.chosen.set(name); this.universe.set(''); }

  protected async run(): Promise<void> {
    const multiverse = this.multiverse();
    const universe = this.universes().some((u) => u.name === this.universe()) ? this.universe() : '';
    this.running.set(true);
    this.error.set(null);
    try {
      const view = await this.api.get<DoctorView>('doctor', { multiverse, universe: universe || undefined });
      this.report.set({ of: universe ? `${multiverse} / ${universe}` : multiverse, view });
    } catch (e) {
      this.error.set((e as Error).message);
    } finally {
      this.running.set(false);
    }
  }
}
