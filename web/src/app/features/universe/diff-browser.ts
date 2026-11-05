import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, HostListener, Injector, afterNextRender, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { Api } from '../../core/api.service';
import { CommitView, DiffFilesView, FileContentView, FileView, PatchView } from '../../core/models';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { FileViewer } from './file-view';
import { PatchViewer } from './patch-view';
import { STATUS_MARK, fileTree } from './patch';

type Loaded = PatchView | { error: string };
type Content = FileContentView | { error: string };
type View = 'files' | 'commits';
/** How the picked file is shown: the diff, the file as it is now, or markdown rendered. */
type Mode = 'diff' | 'raw' | 'rendered';

/**
 * One change, read the way a review is: its commits in one tab, its files in
 * another. The files are a tree beside a single diff, and picking a file swaps
 * the diff in place — the old one stays until the new one has arrived, so
 * nothing collapses, grows or slides under the pointer. It is read again
 * whenever `tick` changes, and when nothing moved nothing is redrawn, so what
 * somebody is reading keeps its place, its selection and its focus.
 */
@Component({
  selector: 'mv-diff-browser',
  imports: [Icon, ErrorBox, Skeleton, PatchViewer, FileViewer, AgoPipe, CopyButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './diff-browser.html',
  styleUrl: './diff-browser.css',
})
export class DiffBrowser {
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly repo = input.required<string>();
  readonly scope = input.required<string>();
  /** What the repository's row measured the branch against: the files are of the same change. */
  readonly base = input('');
  /** The checkout's folder, which a file's full path starts with. */
  readonly root = input('');
  readonly tick = input(0);
  /** The repositories offered in the expanded toolbar; the parent switches on repoChange. */
  readonly repos = input<string[]>([]);
  readonly repoChange = output<string>();
  /** What to say when nothing changed: the parent's wording for this repository. */
  readonly emptyMessage = input('');

  private readonly api = inject(Api);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly files = signal<FileView[] | null>(null);
  /** The branch's commits, newest first: what the work says of itself. */
  protected readonly commits = signal<CommitView[]>([]);
  protected readonly error = signal<string | null>(null);
  protected readonly view = signal<View>('files');
  /** The file picked in the tree, marked there at once. */
  protected readonly active = signal<string | null>(null);
  /** The file whose diff is on screen: the picked one once its diff is in. */
  protected readonly shown = signal<string | null>(null);
  protected readonly patches = signal<Record<string, Loaded>>({});
  protected readonly collapsed = signal<ReadonlySet<string>>(new Set());
  protected readonly filter = signal('');
  /** How the file is shown. */
  protected readonly mode = signal<Mode>('diff');
  /** Whether the browser is lifted to fill the screen. Kept here so it survives
   * switching repositories — the component is not torn down on a switch, where a
   * parent-owned input would not reach it through the reload. The code tab's
   * "Full view" calls open(); this overlay's own close and Esc clear it. */
  protected readonly expanded = signal(false);
  /** Soft-wrap long lines instead of scrolling them sideways. On by default. */
  protected readonly wrap = signal(true);
  /** The current content of files, for the raw and rendered modes; read on demand. */
  protected readonly contents = signal<Record<string, Content>>({});

  protected readonly mark = STATUS_MARK;
  protected readonly hasCommits = computed(() => this.scope() === 'branch');
  protected readonly navigable = computed(() => (this.files()?.length ?? 0) > 1);
  protected readonly tree = computed(() => fileTree(this.files() ?? [], this.filter().trim(), this.collapsed()));
  protected readonly file = computed(() => this.files()?.find((f) => f.path === this.shown()) ?? null);
  /** The files in the order the tree shows them, which is the order to step through. */
  protected readonly ordered = computed(() =>
    fileTree(this.files() ?? [], '', new Set()).flatMap((r) => (r.kind === 'file' ? [r.file as FileView] : [])));
  protected readonly index = computed(() => this.ordered().findIndex((f) => f.path === this.active()));
  protected readonly fullPath = computed(() => {
    const f = this.file(), root = this.root().replace(/\/+$/, '');
    return f && root ? `${root}/${f.path}` : '';
  });
  protected readonly isMarkdown = computed(() => /\.(md|markdown)$/i.test(this.file()?.path ?? ''));
  /** The shown file's current content, for the raw and rendered modes. */
  protected readonly content = computed<Content | null>(() => { const f = this.file(); return f ? this.contents()[f.path] ?? null : null; });
  /** The file-view only knows raw or rendered; diff never reaches it. */
  protected readonly fileMode = computed<'raw' | 'rendered'>(() => (this.mode() === 'rendered' ? 'rendered' : 'raw'));
  protected readonly contentError = computed(() => { const c = this.content(); return c && 'error' in c ? c.error : null; });
  protected readonly fileText = computed(() => { const c = this.content(); return c && !('error' in c) ? c : null; });

  private sig = '';
  private busy = false;
  private again = false;

  constructor() {
    // Another change is a fresh start.
    effect(() => {
      this.repo(); this.scope();
      untracked(() => {
        this.files.set(null); this.commits.set([]); this.error.set(null); this.sig = '';
        this.collapsed.set(new Set()); this.patches.set({}); this.active.set(null); this.shown.set(null); this.filter.set('');
        if (!this.hasCommits()) this.view.set('files');
        void this.load();
      });
    });
    // The same change, read again.
    effect(() => { this.tick(); untracked(() => { if (this.files() !== null) void this.load(); }); });
    // Rendered is markdown's own: a file that is not markdown falls back to raw.
    effect(() => { if (this.mode() === 'rendered' && !this.isMarkdown()) untracked(() => this.mode.set('raw')); });
    // While the browser fills the screen, lock the page behind it so a scroll
    // lands in the overlay and not in the page under it.
    effect(() => {
      const on = this.expanded();
      untracked(() => lockScroll(on));
    });
    this.destroyRef.onDestroy(() => lockScroll(false));
  }

  private params(extra: Record<string, string> = {}) {
    return { multiverse: this.multiverse(), universe: this.universe(), repo: this.repo(), scope: this.scope(), base: this.scope() === 'branch' ? this.base() : '', ...extra };
  }

  private async load(): Promise<void> {
    // One read at a time; one asked for meanwhile waits, and is run when this ends
    // whatever this one finds (an answer for what is no longer shown is dropped).
    if (this.busy) { this.again = true; return; }
    this.busy = true;
    try {
      do {
        this.again = false;
        await this.readOnce();
      } while (this.again);
    } finally {
      this.busy = false;
    }
  }

  private async readOnce(): Promise<void> {
    const mine = [this.repo(), this.scope()];
    try {
      const d = await this.api.get<DiffFilesView>('universe/diff', this.params());
      if (this.repo() !== mine[0] || this.scope() !== mine[1]) return;
      const sig = JSON.stringify(d);
      if (this.files() && sig === this.sig) return;
      this.sig = sig;
      const files = d.files ?? [];
      this.files.set(files);
      this.commits.set(d.commits ?? []);
      this.error.set(null);
      // What was on screen is as old as the list: drop it, and read the file
      // again — the same one if it is still in the change, else the first.
      this.patches.set({});
      this.contents.set({});
      const keep = files.find((f) => f.path === this.active())?.path ?? this.ordered()[0]?.path ?? null;
      this.active.set(keep);
      if (!keep) { this.shown.set(null); return; }
      await this.fetchPatch(keep);
      if (this.active() === keep) this.shown.set(keep);
    } catch (e) {
      this.error.set((e as Error).message);
    }
  }

  private async fetchPatch(path: string): Promise<void> {
    const repo = this.repo(), scope = this.scope();
    let got: Loaded;
    try { got = await this.api.get<PatchView>('universe/diff', this.params({ file: path })); }
    catch (e) { got = { error: (e as Error).message }; }
    if (repo === this.repo() && scope === this.scope()) this.patches.update((p) => ({ ...p, [path]: got }));
  }

  /**
   * Show one file. The tree marks it at once; the diff beside it is swapped
   * only once the new one is in, so the pane changes once, at its new size.
   * The page moves only when the top of the diff has been scrolled out of
   * sight, and then straight to it rather than by an animation that would
   * chase a height still settling.
   */
  protected async show(path: string): Promise<void> {
    if (path === this.shown()) return;
    this.active.set(path);
    if (!this.patches()[path]) await this.fetchPatch(path);
    if (path !== this.active()) return;   // something else was picked meanwhile
    this.shown.set(path);
    if (this.mode() !== 'diff') void this.ensureContent();
    // Once the new diff is drawn, and before it is painted.
    afterNextRender(() => {
      const pane = this.host.nativeElement.querySelector<HTMLElement>('.pane');
      pane?.querySelector<HTMLElement>('.patch')?.scrollTo({ top: 0, left: 0 });
      const bar = pane?.querySelector<HTMLElement>('.filebar');
      // Only when the bar is stuck, which is when the diff's top is out of sight.
      if (pane && bar && pane.getBoundingClientRect().top < parseFloat(getComputedStyle(bar).top)) pane.scrollIntoView({ block: 'start', behavior: 'auto' });
    }, { injector: this.injector });
  }

  protected step(by: number): void {
    const list = this.ordered(), i = this.index() + by;
    if (i >= 0 && i < list.length) void this.show(list[i].path);
  }

  protected async setMode(m: Mode): Promise<void> {
    this.mode.set(m);
    if (m !== 'diff') await this.ensureContent();
  }

  /** Read the shown file's current content once, for the raw and rendered modes. */
  private async ensureContent(): Promise<void> {
    const f = this.file();
    if (!f || this.contents()[f.path]) return;
    const path = f.path;
    let got: Content;
    try { got = await this.api.get<FileContentView>('universe/file', this.params({ file: path })); }
    catch (e) { got = { error: (e as Error).message }; }
    if (this.file()?.path === path) this.contents.update((c) => ({ ...c, [path]: got }));
  }

  protected toggleWrap(): void { this.wrap.update((w) => !w); }
  protected pickRepo(repo: string): void { if (repo !== this.repo()) this.repoChange.emit(repo); }
  /** Lift the browser to fill the screen; the code tab's "Full view" calls this. */
  open(): void { this.expanded.set(true); }

  @HostListener('document:keydown.escape')
  protected onEscape(): void { if (this.expanded()) this.expanded.set(false); }

  protected fold(full: string): void {
    this.collapsed.update((o) => { const n = new Set(o); if (n.has(full)) n.delete(full); else n.add(full); return n; });
  }

  protected title(f: FileView): string { return f.oldPath ? `${f.oldPath} → ${f.path}` : f.path; }
}

/** Hold or release the page scroll, so a scroll over the full-screen overlay stays in it. */
function lockScroll(on: boolean): void {
  try {
    document.documentElement.style.overflow = on ? 'hidden' : '';
  } catch { /* no document, e.g. server-side */ }
}
