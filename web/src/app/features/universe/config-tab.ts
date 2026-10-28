import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { ConfigView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { copyText, plural } from '../../core/util';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { Toasts } from '../../core/toast.service';
import { DiffLine, diffLines } from './line-diff';
import { UniverseStore } from './universe.store';

type View = 'current' | 'started' | 'changes';

/**
 * The manifest a universe uses. The file can be edited after the universe was
 * started and the universe goes on running on what it started with, so this says
 * which is which: the manifest as it is, as it was when the universe was last
 * started, and what changed between them, with the problems loading either would meet.
 */
@Component({
  selector: 'mv-config-tab',
  imports: [AgoPipe, Badge, CopyButton, EmptyState, ErrorBox, Icon, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './config-tab.html',
  styleUrl: './config-tab.css',
})
export class ConfigTab {
  private readonly api = inject(Api);
  private readonly toasts = inject(Toasts);
  protected readonly store = inject(UniverseStore);

  protected readonly data = signal<ConfigView | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly choice = signal<View | null>(null);
  protected readonly plural = plural;

  /** What is shown: what was asked for, else what matters most (the changes, when there are any). */
  protected readonly view = computed<View>(() => {
    const d = this.data();
    const c = this.choice();
    if (c && (c === 'current' || d?.snapshot)) return c;
    return d?.changed && d.snapshot ? 'changes' : 'current';
  });
  protected readonly text = computed(() => (this.view() === 'started' ? this.data()?.snapshot ?? '' : this.data()?.yaml ?? ''));
  protected readonly lines = computed(() => (this.text() ? this.text().replace(/\n$/, '').split('\n') : []));
  protected readonly changes = computed<DiffLine[]>(() => {
    const d = this.data();
    return d?.snapshot ? diffLines(d.snapshot, d.yaml) : [];
  });
  protected readonly counts = computed(() => ({
    added: this.changes().filter((l) => l.op === '+').length,
    removed: this.changes().filter((l) => l.op === '-').length,
  }));

  constructor() {
    effect(() => {
      this.store.query();
      untracked(() => { this.data.set(null); void this.load(); });
    });
    // Starting records the manifest again: what an operation did is read when it ends.
    inject(Ops).finished$.pipe(takeUntilDestroyed()).subscribe(() => void this.load());
  }

  protected pick(v: View): void { this.choice.set(v); }

  protected async copy(): Promise<void> {
    const ok = await copyText(this.text());
    this.toasts.show(ok ? 'Copied the manifest' : 'Copy failed: select the lines and press ctrl-C', ok ? 'ok' : 'bad');
  }

  private async load(): Promise<void> {
    const q = this.store.query();
    if (!q.multiverse || !q.universe) return;
    try {
      const d = await this.api.get<ConfigView>('universe/config', q);
      if (!this.store.isCurrent(q)) return;
      this.data.set(d);
      this.error.set(null);
    } catch (e) {
      if (this.store.isCurrent(q)) this.error.set((e as Error).message);
    }
  }
}
