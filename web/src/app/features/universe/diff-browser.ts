import { ChangeDetectionStrategy, Component, ElementRef, Injector, afterNextRender, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Api } from '../../core/api.service';
import { CommitView, DiffFilesView, FileView, PatchView } from '../../core/models';
import { AgoPipe } from '../../shared/ui/ago.pipe';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { PatchViewer } from './patch-view';
import { STATUS_MARK, fileTree } from './patch';

type Loaded = PatchView | { error: string };
type View = 'files' | 'commits';

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
  imports: [Icon, ErrorBox, Skeleton, PatchViewer, AgoPipe, CopyButton],
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

  private readonly api = inject(Api);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

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

  protected fold(full: string): void {
    this.collapsed.update((o) => { const n = new Set(o); if (n.has(full)) n.delete(full); else n.add(full); return n; });
  }

  protected title(f: FileView): string { return f.oldPath ? `${f.oldPath} → ${f.path}` : f.path; }
}
