import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { Api } from '../../core/api.service';
import { CodeRow, CodeView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { ago, plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { DiffBrowser } from './diff-browser';
import { ReviewChip } from './review-chip';
import { UniverseStore } from './universe.store';

// A universe in view is read again on its own, because a review is merged and a
// branch is pushed from somewhere else; the remotes are asked less often than
// that, since it is the part that costs.
const EVERY_MS = 60_000;
const FETCH_EVERY_MS = 300_000;

type Scope = 'branch' | 'uncommitted';
interface Fact { kind: 'warn' | 'mute' | 'quiet'; text: string; title: string }

/** The change a repository opens on: what the branch amounts to, or, with nothing committed, what is edited. */
const firstScope = (r: CodeRow): Scope => (r.change || !r.uncommitted ? 'branch' : 'uncommitted');

/**
 * How far each repository of the universe has moved from the branch it follows,
 * how much work that is, and the pull or merge request open for it. It is a
 * request of its own, made once the tab is in view: every row costs a few git
 * calls and a question to GitHub or GitLab. Which change is open is in the
 * address, so it can be sent to somebody.
 */
@Component({
  selector: 'mv-code-tab',
  imports: [Badge, CopyButton, ErrorBox, Icon, Skeleton, DiffBrowser, ReviewChip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './code-tab.html',
  styleUrl: './code-tab.css',
})
export class CodeTab {
  readonly repo = input<string>();
  readonly scope = input<string>();

  private readonly api = inject(Api);
  private readonly router = inject(Router);
  private readonly ops = inject(Ops);
  protected readonly store = inject(UniverseStore);

  protected readonly rows = signal<CodeRow[] | null>(null);
  protected readonly fetched = signal<string | undefined>(undefined);
  protected readonly loading = signal(false);
  protected readonly error = signal<string | null>(null);
  /** Bumped when the rows are read again, so the open change is read again with them. */
  protected readonly tick = signal(0);

  /** The repository on show: the one the address names, else the first with something to read. */
  protected readonly row = computed<CodeRow | null>(() => {
    const list = this.rows() ?? [];
    return list.find((r) => r.repo === this.repo()) ?? list.find((r) => r.change || r.uncommitted) ?? list[0] ?? null;
  });
  protected readonly scopes = computed<Scope[]>(() => {
    const r = this.row();
    return r ? ([r.change && 'branch', r.uncommitted && 'uncommitted'].filter(Boolean) as Scope[]) : [];
  });
  protected readonly activeScope = computed<Scope>(() => {
    const r = this.row();
    const want = this.scope() as Scope | undefined;
    return r && want && this.scopes().includes(want) ? want : r ? firstScope(r) : 'branch';
  });
  protected readonly facts = computed<Fact[]>(() => { const r = this.row(); return r ? factsOf(r) : []; });
  protected readonly age = computed(() => (this.loading() ? 'reading…' : this.fetched() ? `remotes read ${ago(this.fetched())}` : ''));
  protected readonly plural = plural;

  private lastFetch = Date.now();
  private busy = false;

  constructor() {
    effect(() => {
      const q = this.store.query();
      if (q.multiverse && q.universe) untracked(() => void this.load(false));
    });
    const timer = setInterval(() => { if (!document.hidden) void this.load(Date.now() - this.lastFetch > FETCH_EVERY_MS); }, EVERY_MS);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
    // A branch was pushed, or a universe updated: read again.
    this.ops.finished$.pipe(takeUntilDestroyed()).subscribe(() => void this.load(false));
  }

  protected reread(): void { void this.load(true); }

  /** Choose a repository, and optionally which of its changes; both live in the address. */
  protected select(repo: string, scope?: Scope): void {
    const r = (this.rows() ?? []).find((x) => x.repo === repo);
    void this.router.navigate([], { queryParams: { repo, scope: scope ?? (r ? firstScope(r) : undefined) }, queryParamsHandling: 'merge', replaceUrl: true });
  }

  /** The arrows move between tabs and Tab leaves the list, as a tab list does. */
  protected arrows(e: KeyboardEvent, count: number, pick: (i: number) => void, current: number): void {
    const to = e.key === 'ArrowRight' ? (current + 1) % count : e.key === 'ArrowLeft' ? (current - 1 + count) % count : e.key === 'Home' ? 0 : e.key === 'End' ? count - 1 : -1;
    if (to < 0) return;
    e.preventDefault();
    pick(to);
    queueMicrotask(() => ((e.currentTarget as HTMLElement).querySelectorAll('[role=tab]')[to] as HTMLElement | undefined)?.focus());
  }

  protected repoIndex(): number { return (this.rows() ?? []).findIndex((r) => r.repo === this.row()?.repo); }
  protected pickRepo(i: number): void { const r = this.rows()?.[i]; if (r) this.select(r.repo); }
  protected scopeIndex(): number { return this.scopes().indexOf(this.activeScope()); }
  protected pickScope(i: number): void { const r = this.row(); if (r) this.select(r.repo, this.scopes()[i]); }

  protected scopeLabel(s: Scope): string {
    const r = this.row();
    return s === 'branch' ? `Changes against ${r?.base ?? 'its base'}` : 'Not committed';
  }
  protected scopeStat(s: Scope) { return s === 'branch' ? this.row()?.change : this.row()?.uncommitted; }

  private async load(fetch: boolean): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.loading.set(true);
    const q = this.store.query();
    try {
      const d = await this.api.get<CodeView>('universe/code', { ...q, fetch });
      if (q.universe !== this.store.name()) return;
      this.rows.set(d.rows ?? []);
      this.fetched.set(d.fetched);
      this.error.set(null);
      if (fetch) this.lastFetch = Date.now();
      this.tick.update((n) => n + 1);
    } catch (e) {
      this.error.set((e as Error).message);
    } finally {
      this.busy = false;
      this.loading.set(false);
    }
  }
}

/**
 * What became of a repository, each fact in words and on its own: a terse
 * "+31 −4 3 files vs main · 2 not pushed" makes somebody work out which number
 * belongs to which question. What changed is in the tabs next to these.
 */
function factsOf(r: CodeRow): Fact[] {
  const out: Fact[] = [];
  if (!r.change) {
    if (r.baseUnknown) {
      // Not the same as nothing changed: the base is not in the clone to compare with.
      out.push({ kind: 'warn', text: `Could not compare with ${r.base}`, title: `origin/${r.base} is not in this clone, perhaps not fetched yet. Reread asks the remotes.` });
    } else if (r.base) {
      out.push({ kind: 'quiet', text: `No changes against ${r.base}`, title: `This branch has nothing that ${r.base} lacks.` });
    } else {
      out.push({ kind: 'quiet', text: 'Nothing to compare it with', title: 'This repository has no usable origin to measure against.' });
    }
  }
  if (r.ahead && r.follows) {
    out.push(r.follows === r.base
      ? { kind: 'mute', text: `${plural(r.ahead, 'commit')}, branch not pushed yet`, title: `These commits are only on this machine: ${r.follows} on origin does not have them.` }
      : { kind: 'mute', text: `${plural(r.ahead, 'commit')} not pushed`, title: `These commits are only on this machine, not on origin/${r.follows}${r.work ? `: ${plural(r.work.files, 'file')}, +${r.work.added} −${r.work.removed}.` : '.'}` });
  }
  if (r.behind) {
    out.push({ kind: 'warn', text: `${plural(r.behind, 'commit')} behind ${r.follows}`, title: `It lacks ${plural(r.behind, 'commit')} of ${r.follows}, as of the last fetch. Update code brings them in.` });
  }
  return out;
}
