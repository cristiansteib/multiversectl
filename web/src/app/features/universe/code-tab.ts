import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { Api } from '../../core/api.service';
import { CodeRow, CodeView, DiffView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { ago, landedHint, plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { PrChip } from '../../shared/ui/pr-chip';
import { PrIcon } from '../../shared/ui/pr-icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { DiffBrowser } from './diff-browser';
import { UniverseStore } from './universe.store';

// A universe in view is read again on its own, because a review is merged and a
// branch is pushed from somewhere else; the remotes are asked less often than
// that, since it is the part that costs.
const EVERY_MS = 60_000;
const FETCH_EVERY_MS = 300_000;

type Scope = 'branch' | 'uncommitted';
interface Fact { kind: 'warn' | 'mute' | 'quiet'; text: string; title: string }

/**
 * Whether a repository has work still to be done: a change its base has not
 * taken yet, or edits not committed. A branch that landed has neither, however
 * much it amounts to — its change is the record of what was merged.
 */
const outstanding = (r: CodeRow): boolean => Boolean((r.change && !r.landed) || r.uncommitted);

/** The change a repository opens on: the one with work still in it, else what the branch amounts to. */
const firstScope = (r: CodeRow): Scope => ((r.change && !r.landed) || !r.uncommitted ? 'branch' : 'uncommitted');

/**
 * How far each repository of the universe has moved from the branch it follows,
 * how much work that is, and the pull or merge request open for it. It is a
 * request of its own, made once the tab is in view: every row costs a few git
 * calls and a question to GitHub or GitLab. Which change is open is in the
 * address, so it can be sent to somebody.
 */
@Component({
  selector: 'mv-code-tab',
  imports: [Badge, CopyButton, ErrorBox, Icon, Skeleton, DiffBrowser, PrChip, PrIcon],
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

  /** An order dragged by hand, by repository name. Null means the automatic one. */
  protected readonly manual = signal<string[] | null>(null);
  /** The tab being dragged, and the one it would land on. */
  protected readonly dragging = signal<string | null>(null);
  protected readonly over = signal<string | null>(null);
  /** What just happened to the order, for a reader who cannot see it move. */
  protected readonly moved = signal('');

  /**
   * The tabs, in the order they are shown.
   *
   * By default the repositories that moved come first and the quiet ones
   * after: a universe can hold a dozen repositories and usually only one or
   * two of them are the work, so the ones with something to read should not be
   * hunted for behind the ones with nothing. Within each group the server's
   * order is kept — the sort is stable, so repositories that are equally
   * interesting stay in the order the manifest lists them and the tabs do not
   * reshuffle on every read.
   *
   * Once the order has been dragged by hand that order wins, because the
   * person arranging the tabs knows better than the rule does. A repository
   * cloned since then is not in the saved order and goes to the end, where it
   * can be dragged: appending is predictable, whereas threading it back into
   * the automatic order would move tabs someone deliberately placed.
   */
  protected readonly ordered = computed<CodeRow[]>(() => {
    const auto = [...(this.rows() ?? [])].sort((a, b) => Number(outstanding(b)) - Number(outstanding(a)));
    const by = this.manual();
    if (!by) return auto;
    const placed = by.map((name) => auto.find((r) => r.repo === name)).filter((r): r is CodeRow => !!r);
    return [...placed, ...auto.filter((r) => !by.includes(r.repo))];
  });

  /** The repository on show: the one the address names, else the first with something to read. */
  protected readonly row = computed<CodeRow | null>(() => {
    const list = this.ordered();
    return list.find((r) => r.repo === this.repo()) ?? list[0] ?? null;
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
  /**
   * The counter a tab carries, and which of three claims it is making. A
   * repository can have moved in more than one way and the tab shows one of
   * them: `change` is what the branch amounts to against what it will be merged
   * into, and it is absent whenever there is no base to measure against — a
   * branch with nothing committed yet, a clone with no usable origin, a
   * repository whose review could not be asked. Those repositories were showing
   * no counter at all despite being edited, which is exactly the case someone is
   * looking for.
   *
   * So: whichever of the two is work still to be done, and a change the base
   * holds already only when there is nothing left. The kinds are told apart by
   * weight in the tab and named for a screen reader, because +12 −3 of work to
   * review, +12 −3 of unsaved edits and +12 −3 that landed last week are three
   * different claims.
   */
  protected counts(r: CodeRow): { d: DiffView; kind: 'branch' | 'uncommitted' | 'landed' } | null {
    if (r.change && !r.landed) return { d: r.change, kind: 'branch' };
    if (r.uncommitted) return { d: r.uncommitted, kind: 'uncommitted' };
    return r.change ? { d: r.change, kind: 'landed' } : null;
  }

  /** Whether the rows have been read, as against shown from what was kept. */
  protected readonly loaded = signal(false);

  /**
   * The best rows available without asking, in order of how much they know:
   *
   *  1. what the store already read for this universe — the same rows, complete;
   *  2. what the last visit left behind, kept for the session;
   *  3. the universe's own checkouts, which the fleet already carries.
   *
   * The third is the one that matters on a first visit. A checkout names the
   * repository, the branch it is on, and whether it is ahead, behind or edited
   * — everything except the diff stats, which are what the reading is slow for.
   * The fleet is on the page before the code is asked for, so the tabs can be
   * there and clickable from the start and the counters can land into them.
   *
   * Null only when even the names are unknown, which is the one case left that
   * earns a skeleton.
   */
  private known(q: { multiverse: string; universe: string }): CodeRow[] | null {
    const fromStore = [...this.store.code().values()];
    if (fromStore.length) return fromStore;
    const kept = this.readKept(q);
    if (kept?.length) return kept;
    const checkouts = this.store.universe()?.checkouts ?? [];
    return checkouts.length
      ? checkouts.map((c) => ({ repo: c.repo, branch: c.branch, follows: c.follows,
                                behind: c.behind, ahead: c.ahead, dirty: c.dirty }))
      : null;
  }

  private keptKey(q: { multiverse: string; universe: string }): string {
    return `code-tab-rows:${q.multiverse}:${q.universe}`;
  }

  /** The rows from the last visit to this universe, for the first paint only. */
  private readKept(q: { multiverse: string; universe: string }): CodeRow[] | null {
    try {
      const raw = sessionStorage.getItem(this.keptKey(q));
      const v = raw ? (JSON.parse(raw) as unknown) : null;
      return Array.isArray(v) && v.every((r) => r && typeof (r as CodeRow).repo === 'string')
        ? (v as CodeRow[]) : null;
    } catch { return null; /* private mode, or something else wrote the key */ }
  }

  /**
   * Keep the rows for the next visit. sessionStorage and not localStorage on
   * purpose: these are a snapshot of a working tree, true for minutes at most,
   * and a counter from last week shown as though it were current would be worse
   * than a skeleton. A session is about as long as it stays plausible.
   */
  private keep(q: { multiverse: string; universe: string }, rows: CodeRow[]): void {
    try { sessionStorage.setItem(this.keptKey(q), JSON.stringify(rows)); } catch { /* private mode */ }
  }

  protected readonly landedHint = landedHint;
  protected readonly facts = computed<Fact[]>(() => { const r = this.row(); return r ? factsOf(r) : []; });
  protected readonly age = computed(() => (this.loading() ? 'reading…' : this.fetched() ? `remotes read ${ago(this.fetched())}` : ''));
  protected readonly plural = plural;

  private lastFetch = Date.now();
  private busy = false;
  private again = false;
  private againFetch = false;

  constructor() {
    effect(() => {
      const q = this.store.query();
      if (q.multiverse && q.universe) untracked(() => {
        // Show what is already known before asking for it again. Reading a
        // universe's code costs a handful of git calls per repository and, for
        // each, a question to GitHub or GitLab; clearing the rows first meant
        // every visit began with a skeleton and nothing to click for as long as
        // that took. See known() for what is shown instead. The read then
        // replaces it, and `loaded` says which of the two is on screen, so a
        // counter kept from before is never mistaken for a fresh one.
        this.loaded.set(false);
        this.rows.set(this.known(q));
        // The order is saved per multiverse, so it is read again when that
        // changes and not merely once when the tab is first built.
        this.loadOrder();
        void this.load(false);
      });
    });
    // The fleet and the store's own read arrive on their own schedule. Until
    // this tab's read lands, take whichever of them says more: the tabs appear
    // as soon as the universe is known and gain their counters when the stats
    // are, rather than waiting for one request to answer both questions.
    effect(() => {
      const fromStore = this.store.code();
      const better = this.known(this.store.query());
      if (this.loaded() || !better?.length) return;
      untracked(() => {
        this.rows.set(better);
        // The store reads the same endpoint, so when its rows are in, the stats
        // are in: a repository showing no counter from here on is a repository
        // with nothing changed, not one still being read. Without this the
        // "still reading" dot stayed on the quiet repositories until this tab's
        // own duplicate read answered, saying the wrong thing for seconds.
        if (fromStore.size) this.loaded.set(true);
      });
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
    if (e.altKey) return;   // Alt with an arrow reorders; see reorder().
    const to = e.key === 'ArrowRight' ? (current + 1) % count : e.key === 'ArrowLeft' ? (current - 1 + count) % count : e.key === 'Home' ? 0 : e.key === 'End' ? count - 1 : -1;
    if (to < 0) return;
    e.preventDefault();
    pick(to);
    queueMicrotask(() => ((e.currentTarget as HTMLElement).querySelectorAll('[role=tab]')[to] as HTMLElement | undefined)?.focus());
  }

  /* ---- arranging the tabs by hand ------------------------------------
     Dragging is the obvious way and the only one some people will look for,
     but it cannot be the only one: a tab that can only be moved with a mouse
     cannot be moved by someone using a keyboard. Alt with the arrows does the
     same thing, and says what it did out loud, because a reader who cannot see
     the tabs move has nothing else to go on. Plain arrows still move between
     tabs — Alt is what makes it a reorder instead. */

  private key(): string { return `code-tab-order:${this.store.multiverse()}`; }

  /** Read the order back for this multiverse. The repositories are the same in
   *  every universe of one, so an order arranged in one applies to them all. */
  private loadOrder(): void {
    try {
      const raw = localStorage.getItem(this.key());
      const by = raw ? (JSON.parse(raw) as unknown) : null;
      this.manual.set(Array.isArray(by) && by.every((x) => typeof x === 'string') ? (by as string[]) : null);
    } catch { this.manual.set(null); /* private mode, or something else wrote the key */ }
  }

  private saveOrder(by: string[] | null): void {
    this.manual.set(by);
    try {
      if (by) localStorage.setItem(this.key(), JSON.stringify(by));
      else localStorage.removeItem(this.key());
    } catch { /* private mode */ }
  }

  /** Move a repository to where another one is, and remember it. */
  private move(repo: string, toRepo: string): void {
    const names = this.ordered().map((r) => r.repo);
    const from = names.indexOf(repo);
    const to = names.indexOf(toRepo);
    if (from < 0 || to < 0 || from === to) return;
    names.splice(to, 0, ...names.splice(from, 1));
    this.saveOrder(names);
    this.moved.set(`${repo} moved to position ${to + 1} of ${names.length}`);
  }

  protected dragStart(e: DragEvent, repo: string): void {
    this.dragging.set(repo);
    // Firefox starts no drag at all without data on the transfer.
    e.dataTransfer?.setData('text/plain', repo);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }

  protected dragOver(e: DragEvent, repo: string): void {
    if (!this.dragging() || this.dragging() === repo) return;
    // Without preventDefault the browser refuses the drop and nothing lands.
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    this.over.set(repo);
  }

  protected drop(e: DragEvent, repo: string): void {
    e.preventDefault();
    const from = this.dragging() ?? e.dataTransfer?.getData('text/plain');
    if (from) this.move(from, repo);
    this.dragEnd();
  }

  protected dragEnd(): void { this.dragging.set(null); this.over.set(null); }

  /**
   * Whether the dragged tab would land after the one it is over rather than
   * before it. Dragging rightwards lands after, leftwards lands before — that
   * is what move() does once the tab has been lifted out of the list — so the
   * line marking the drop has to be drawn on the matching edge or it points at
   * the wrong gap.
   */
  protected dropsAfter(repo: string): boolean {
    const names = this.ordered().map((r) => r.repo);
    const from = names.indexOf(this.dragging() ?? '');
    return from >= 0 && from < names.indexOf(repo);
  }

  /** Alt with an arrow moves the tab itself, which is what dragging does. */
  protected reorder(e: KeyboardEvent, repo: string): boolean {
    if (!e.altKey || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return false;
    const names = this.ordered().map((r) => r.repo);
    const i = names.indexOf(repo);
    const to = e.key === 'ArrowRight' ? i + 1 : i - 1;
    if (i < 0 || to < 0 || to >= names.length) return true;   // handled: at the end, stay put
    e.preventDefault();
    e.stopPropagation();
    this.move(repo, names[to]!);
    queueMicrotask(() => ((e.currentTarget as HTMLElement | null)?.focus()));
    return true;
  }

  /** Back to the automatic order. Only offered once there is one to undo. */
  protected resetOrder(): void {
    this.saveOrder(null);
    this.moved.set('Tab order reset: repositories with changes first');
  }

  protected repoIndex(): number { return this.ordered().findIndex((r) => r.repo === this.row()?.repo); }
  protected pickRepo(i: number): void { const r = this.ordered()[i]; if (r) this.select(r.repo); }
  protected scopeIndex(): number { return this.scopes().indexOf(this.activeScope()); }
  protected pickScope(i: number): void { const r = this.row(); if (r) this.select(r.repo, this.scopes()[i]); }

  protected scopeLabel(s: Scope): string {
    const r = this.row();
    if (s !== 'branch') return 'Not committed';
    return r?.landed ? `What landed in ${r.base ?? 'its base'}` : `Changes against ${r?.base ?? 'its base'}`;
  }

  protected scopeStat(s: Scope) { return s === 'branch' ? this.row()?.change : this.row()?.uncommitted; }

  /** What the panel says with no change to read. A landed branch has none for a reason. */
  protected readonly nothingToRead = computed(() => {
    const r = this.row();
    if (!r) return '';
    return r.landed
      ? `Nothing of ${r.repo} is left to merge: ${r.base ?? 'its base'} holds its work already.`
      : `Nothing changed in ${r.repo}${r.base ? ' against ' + r.base : ''}.`;
  });

  private async load(fetch: boolean): Promise<void> {
    // One read at a time; one asked for meanwhile (another universe, a fetch) is
    // run when this ends, so what is shown is never what was asked for before.
    if (this.busy) { this.again = true; this.againFetch ||= fetch; return; }
    this.busy = true;
    this.loading.set(true);
    try {
      let f = fetch;
      do {
        this.again = false;
        this.againFetch = false;
        await this.readOnce(f);
        f = this.againFetch;
      } while (this.again);
    } finally {
      this.busy = false;
      this.loading.set(false);
    }
  }

  private async readOnce(fetch: boolean): Promise<void> {
    const q = this.store.query();
    try {
      const d = await this.api.get<CodeView>('universe/code', { ...q, fetch });
      if (!this.store.isCurrent(q)) return;
      const rows = d.rows ?? [];
      this.rows.set(rows);
      this.loaded.set(true);
      this.keep(q, rows);
      this.fetched.set(d.fetched);
      this.error.set(null);
      if (fetch) this.lastFetch = Date.now();
      this.tick.update((n) => n + 1);
    } catch (e) {
      if (this.store.isCurrent(q)) this.error.set((e as Error).message);
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
  if (r.landed) {
    // Said whether or not a change is left to read: a merge commit leaves the
    // base holding the branch's own commits, and then there is none.
    out.push({ kind: 'quiet', text: `Nothing left to merge into ${r.base ?? 'its base'}`,
               title: `${r.base ?? 'Its base'} holds every change of this branch already${r.change ? ', and what is below is the record of it' : ''}.` });
  } else if (!r.change) {
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
