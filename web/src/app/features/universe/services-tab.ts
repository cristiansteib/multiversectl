import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { ContainerView, ServiceView, StatView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { plural } from '../../core/util';
import { Sparkline } from '../../shared/chart/sparkline';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { LogViewer } from './log-viewer';
import { isWebUrl } from './patch';
import { UniverseStore } from './universe.store';

const STATS_MS = 3000;
const SPARK_POINTS = 40;
const STATE_DOT: Record<string, string> = { running: 'ok', healthy: 'ok', exited: 'warn', unhealthy: 'bad', restarting: 'warn', created: 'warn' };

/**
 * What runs in the universe: its apps and services as the manifest names them,
 * then its containers with what each is using, then a log. Everything here that
 * polls does it only while the page is in view and stops when it is left, so a
 * universe nobody is looking at costs nothing.
 */
@Component({
  selector: 'mv-services-tab',
  imports: [Badge, CopyButton, EmptyState, ErrorBox, Icon, Skeleton, Sparkline, LogViewer],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './services-tab.html',
  styleUrl: './services-tab.css',
})
export class ServicesTab {
  /** `log=app/service` in the address opens straight on that log. */
  readonly log = input<string>();

  private readonly api = inject(Api);
  private readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  protected readonly store = inject(UniverseStore);
  private readonly destroyRef = inject(DestroyRef);
  private readonly logPanel = viewChild(LogViewer, { read: ElementRef });

  protected readonly rows = signal<ContainerView[] | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly stats = signal<Record<string, StatView>>({});
  protected readonly history = signal<Record<string, number[]>>({});
  protected readonly selected = signal('');
  /** The service whose restart is waiting for a second press. */
  protected readonly confirming = signal<string | null>(null);

  protected readonly u = this.store.universe;
  protected readonly running = computed(() => (this.rows() ?? []).filter((r) => r.running).length);
  protected readonly busy = computed(() => this.ops.busy(this.store.multiverse()));
  protected readonly choices = computed(() => (this.rows() ?? []).map((r) => ({ key: `${r.app}/${r.service}`, running: r.running })));
  protected readonly readOnly = this.events.readOnly;
  protected readonly min = (n: number) => Math.min(100, Math.max(0, n));
  protected readonly webUrl = isWebUrl;
  protected readonly plural = plural;
  protected readonly dot = (state: string) => STATE_DOT[state] ?? '';
  protected readonly tone = (pct: number) => (pct >= 85 ? 'bad' : pct >= 60 ? 'warn' : 'ok');

  private confirmTimer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    // Read the containers when the universe is known, and again when the fleet
    // changes or an operation of this multiverse ends: a service that was just
    // restarted should not still read "Exited".
    effect(() => {
      this.events.fleet();
      const q = this.store.query();
      if (q.multiverse && q.universe) untracked(() => void this.loadRows());
    });
    this.ops.finished$.pipe(takeUntilDestroyed()).subscribe(() => void this.loadRows());

    const timer = setInterval(() => void this.pollStats(), STATS_MS);
    this.destroyRef.onDestroy(() => { clearInterval(timer); clearTimeout(this.confirmTimer); });

    effect(() => { const want = this.log(); if (want) untracked(() => this.selected.set(want)); });
  }

  protected svcTitle(s: ServiceView): string {
    return [s.state, s.detail, s.published && `publishes ${s.published}`].filter(Boolean).join(' · ');
  }

  protected showLog(key: string): void {
    this.selected.set(key);
    queueMicrotask(() => this.logPanel()?.nativeElement.scrollIntoView({ behavior: 'smooth', block: 'start' }));
  }

  protected restart(r: ContainerView): void {
    const key = `${r.app}/${r.service}`;
    if (this.confirming() !== key) {
      this.confirming.set(key);
      clearTimeout(this.confirmTimer);
      this.confirmTimer = setTimeout(() => { if (this.confirming() === key) this.confirming.set(null); }, 4000);
      return;
    }
    this.confirming.set(null);
    void this.ops.runOrSay({ action: 'restart', multiverse: this.store.multiverse(), universe: this.store.name(), app: r.app, service: r.service });
  }

  protected start(): void {
    void this.ops.runOrSay({ action: 'up', multiverse: this.store.multiverse(), universe: this.store.name() });
  }

  /** Where this container is reached: the addresses the proxy routes to this very service. */
  protected urlsFor(r: ContainerView): string[] {
    const app = this.u()?.apps.find((a) => a.key === r.app);
    return (app?.urls ?? []).filter((x) => x.service === r.service && isWebUrl(x.url)).map((x) => x.url);
  }

  protected shell(r: ContainerView): string {
    return `multiversectl universe exec ${this.store.name()} ${r.app} --service ${r.service} -- sh`;
  }

  private async loadRows(): Promise<void> {
    const q = this.store.query();
    try {
      const r = await this.api.get<{ containers: ContainerView[] | null }>('universe/containers', q);
      if (q.universe !== this.store.name()) return;
      this.rows.set(r.containers ?? []);
      this.error.set(null);
    } catch (e) {
      this.error.set((e as Error).message);
      if (this.rows() === null) this.rows.set([]);
    }
  }

  private async pollStats(): Promise<void> {
    if (document.hidden || !(this.rows() ?? []).some((r) => r.running)) return;
    const q = this.store.query();
    try {
      const r = await this.api.get<{ stats: StatView[] | null }>('universe/stats', q);
      const list = r.stats ?? [];
      this.stats.set(Object.fromEntries(list.map((s) => [s.name, s])));
      this.history.update((h) => {
        const next = { ...h };
        for (const s of list) next[s.name] = [...(next[s.name] ?? []), s.cpu].slice(-SPARK_POINTS);
        return next;
      });
    } catch { /* the next reading tries again */ }
  }
}
