import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Api } from '../../core/api.service';
import { DiffFilesView, FileView, PatchView } from '../../core/models';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { PatchViewer } from './patch-view';
import { STATUS_MARK, fileTree } from './patch';

type Loaded = PatchView | { error: string };

/**
 * The files of one change and the diff of each. Opening a file closes the
 * others unless they are pinned, because a page of diffs nobody can find the top
 * of is worse than one click to reopen. It is read again whenever `tick`
 * changes, and when nothing moved nothing is redrawn, so what somebody is
 * reading keeps its place, its selection and its focus.
 */
@Component({
  selector: 'mv-diff-browser',
  imports: [Icon, ErrorBox, Skeleton, PatchViewer],
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
  readonly tick = input(0);

  private readonly api = inject(Api);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  protected readonly files = signal<FileView[] | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly open = signal<ReadonlySet<string>>(new Set());
  protected readonly pinned = signal<ReadonlySet<string>>(new Set());
  protected readonly collapsed = signal<ReadonlySet<string>>(new Set());
  protected readonly patches = signal<Record<string, Loaded>>({});
  protected readonly active = signal<string | null>(null);
  protected readonly filter = signal('');

  protected readonly mark = STATUS_MARK;
  protected readonly navigable = computed(() => (this.files()?.length ?? 0) > 1);
  protected readonly tree = computed(() => fileTree(this.files() ?? [], this.filter().trim(), this.collapsed()));

  private sig = '';
  private busy = false;
  private again = false;

  constructor() {
    // Another change is a fresh start.
    effect(() => {
      this.repo(); this.scope();
      untracked(() => {
        this.files.set(null); this.error.set(null); this.sig = '';
        this.open.set(new Set()); this.pinned.set(new Set()); this.collapsed.set(new Set());
        this.patches.set({}); this.active.set(null); this.filter.set('');
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
      this.error.set(null);
      const paths = new Set(files.map((f) => f.path));
      this.open.update((o) => new Set([...o].filter((p) => paths.has(p))));
      this.pinned.update((o) => new Set([...o].filter((p) => paths.has(p))));
      this.patches.set({});
      // The first change of the list is what somebody came to see.
      if (!this.open().size && files.length) { this.open.set(new Set([files[0].path])); this.active.set(files[0].path); }
      if (this.active() && !paths.has(this.active()!)) this.active.set(null);
      // What was open is as old as the list: read again, so it cannot disagree.
      await Promise.all([...this.open()].map((p) => this.fetchPatch(p)));
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

  protected async show(path: string, scroll: boolean): Promise<void> {
    this.active.set(path);
    if (!this.open().has(path)) {
      const pinned = this.pinned();
      this.open.set(new Set([...this.open()].filter((p) => pinned.has(p)).concat(path)));
    }
    if (scroll) {
      requestAnimationFrame(() => this.host.nativeElement.querySelector(`[data-path="${CSS.escape(path)}"]`)?.scrollIntoView({ block: 'start', behavior: 'smooth' }));
    }
    if (!this.patches()[path]) await this.fetchPatch(path);
  }

  protected async toggle(path: string): Promise<void> {
    if (this.open().has(path)) {
      this.open.update((o) => { const n = new Set(o); n.delete(path); return n; });
      this.pinned.update((o) => { const n = new Set(o); n.delete(path); return n; });
      return;
    }
    await this.show(path, false);
  }

  protected pin(path: string): void {
    this.pinned.update((o) => { const n = new Set(o); if (n.has(path)) n.delete(path); else n.add(path); return n; });
  }

  protected fold(full: string): void {
    this.collapsed.update((o) => { const n = new Set(o); if (n.has(full)) n.delete(full); else n.add(full); return n; });
  }

  protected title(f: FileView): string { return f.oldPath ? `${f.oldPath} → ${f.path}` : f.path; }
}
