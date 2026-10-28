import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, signal, viewChild } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { CatalogView } from '../../core/models';
import { Toasts } from '../../core/toast.service';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { CatalogTree } from './catalog-tree';
import { FetchProgress } from './fetch-progress';
import { Step, stepKey, tally } from './catalog.model';

/** Your index of multiverses, as a tree, and the way to read it from the remotes. */
@Component({
  selector: 'mv-catalog-page',
  imports: [AgoPipe, Badge, CatalogTree, CopyButton, EmptyState, ErrorBox, FetchProgress, Icon, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './catalog-page.html',
  styleUrl: './catalog-page.css',
})
export class CatalogPage {
  private readonly api = inject(Api);
  private readonly events = inject(Events);
  private readonly toasts = inject(Toasts);

  protected readonly data = signal<CatalogView | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly fetching = signal(false);
  protected readonly fetchedAt = signal<string | null>(null);
  protected readonly filter = signal('');
  protected readonly steps = signal<ReadonlyMap<string, Step>>(new Map());
  protected readonly noRoot = computed(() => /no root catalog/i.test(this.error() ?? ''));
  protected readonly tally = computed(() => {
    const d = this.data();
    return d ? tally(d.root) : null;
  });
  protected readonly empty = computed(() => {
    const d = this.data();
    return !!d && !(d.root.children?.length);
  });
  protected readonly showProgress = computed(() => this.fetching() && this.steps().size > 0);

  private readonly search = viewChild<ElementRef<HTMLInputElement>>('search');
  private readonly tree = viewChild(CatalogTree);

  constructor() {
    // Catalogs are read at the same time, so each reports as it answers.
    this.events.catalogProgress$.pipe(takeUntilDestroyed()).subscribe((e) => {
      if (!this.fetching()) return;
      const next = new Map(this.steps());
      next.set(stepKey(e.repo, e.ref), { repo: e.repo, phase: e.phase, detail: e.detail, ms: e.ms });
      this.steps.set(next);
    });
    void this.load(false);
  }

  /** `/` goes to the filter from anywhere on the page that is not already a text field. */
  @HostListener('document:keydown', ['$event'])
  protected slash(e: KeyboardEvent): void {
    if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey) return;
    if (/input|textarea|select/i.test((document.activeElement as HTMLElement | null)?.tagName ?? '')) return;
    e.preventDefault();
    this.search()?.nativeElement.focus();
  }

  protected async load(fetch: boolean): Promise<void> {
    if (fetch) { this.fetching.set(true); this.steps.set(new Map()); }
    try {
      this.data.set(await this.api.get<CatalogView>('catalog', { fetch }));
      this.error.set(null);
      if (fetch) { this.fetchedAt.set(new Date().toISOString()); this.toasts.show('Catalogs read from their remotes', 'ok'); }
    } catch (e) {
      this.error.set((e as Error).message);
    } finally {
      this.fetching.set(false);
    }
  }

  protected setFilter(v: string): void { this.filter.set(v); }
  protected expandAll(): void { this.tree()?.expandAll(); }
  protected collapseAll(): void { this.tree()?.collapseAll(); }
}
