import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, signal, untracked, viewChild } from '@angular/core';
import { Api } from '../../core/api.service';
import { NodeView, PlanView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { ErrorBox } from '../../shared/ui/error-box';
import { Modal } from '../../shared/ui/modal';
import { Skeleton } from '../../shared/ui/skeleton';

/**
 * Installing from the catalog: what would be set up and where, with the option
 * to leave some out, and the same thing as a command to copy. It is the
 * operation `multiversectl start <path> --install-only` is, run here.
 */
@Component({
  selector: 'mv-install-dialog',
  imports: [Modal, Badge, CopyButton, ErrorBox, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './install-dialog.html',
  styleUrl: './install-dialog.css',
})
export class InstallDialog {
  private readonly api = inject(Api);
  private readonly ops = inject(Ops);

  readonly node = input.required<NodeView>();
  readonly closed = output<void>();

  private readonly modal = viewChild.required(Modal);
  protected readonly plan = signal<PlanView | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal(false);
  /** Paths unticked, rather than ticked: what the plan adds later is in. */
  protected readonly left = signal<ReadonlySet<string>>(new Set());
  protected readonly root = signal<string | null>(null);

  protected readonly title = computed(() => (this.node().kind === 'multiverse' ? `Install ${this.node().name}` : `Install what is under ${this.node().name}`));
  protected readonly todo = computed(() => (this.plan()?.targets ?? []).filter((t) => !t.skip && !this.left().has(t.path ?? t.name)));
  protected readonly skipped = computed(() => (this.plan()?.targets ?? []).filter((t) => t.skip));
  protected readonly choices = computed(() => (this.plan()?.targets ?? []).filter((t) => !t.skip));
  /** Where it lives can be chosen when one multiverse is installed. */
  protected readonly single = computed(() => (this.todo().length === 1 ? this.todo()[0] : null));
  protected readonly where = computed(() => this.root() ?? this.single()?.root ?? '');
  protected readonly plural = plural;

  /** The same as a command, for a terminal: what is left out and where it lives are in it. */
  protected readonly command = computed(() => {
    const parts = ['multiversectl'];
    const one = this.single();
    if (one && this.root() && this.root() !== one.root) parts.push('--root', this.quote(this.root()!));
    parts.push('start', this.quote(this.node().path), '--install-only');
    for (const p of this.left()) parts.push('--exclude', this.quote(p));
    return parts.join(' ');
  });

  constructor() {
    // The plan of the node the dialog is for, read once it is known.
    effect(() => { const path = this.node().path; untracked(() => void this.load(path)); });
  }

  protected toggle(path: string, on: boolean): void {
    const next = new Set(this.left());
    if (on) next.delete(path); else next.add(path);
    this.left.set(next);
  }

  protected async submit(e: Event): Promise<void> {
    e.preventDefault();
    if (!this.todo().length || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const one = this.single();
      await this.ops.run({
        action: 'install', path: this.node().path, exclude: [...this.left()],
        root: one && this.root() && this.root() !== one.root ? this.root()! : undefined,
      });
      this.modal().close();
    } catch (err) {
      this.error.set((err as Error).message);
    } finally {
      this.busy.set(false);
    }
  }

  private async load(path: string): Promise<void> {
    try {
      this.plan.set(await this.api.get<PlanView>('catalog/plan', { path }));
    } catch (e) {
      this.error.set((e as Error).message);
      this.plan.set({ targets: [] });
    }
  }

  private quote(s: string): string { return /^[\w@%+=:,./~-]+$/.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`; }
}
