import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal, viewChild } from '@angular/core';
import { Api } from '../../core/api.service';
import { BranchView, NextView, OpRequest } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { ErrorBox } from '../ui/error-box';
import { Modal } from '../ui/modal';

type Mode = 'new' | 'checkout';
let ids = 0;

/**
 * Two ways to make a universe, and they never mix: a fresh one starts branches
 * that exist nowhere, so it cannot land on anyone's work; a checkout one works
 * on a branch that already exists.
 */
@Component({
  selector: 'mv-new-universe-dialog',
  imports: [Modal, ErrorBox],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './new-universe-dialog.html',
  styles: `.modes { margin-bottom: 14px } small { color: var(--muted) } .hint { min-height: 1.4em }`,
})
export class NewUniverseDialog {
  private readonly api = inject(Api);
  private readonly ops = inject(Ops);
  readonly multiverse = input.required<string>();
  readonly closed = output<void>();

  private readonly modal = viewChild.required(Modal);
  protected readonly listId = `branch-list-${ids++}`;
  protected readonly mode = signal<Mode>('new');
  protected readonly name = signal('');
  protected readonly base = signal('');
  protected readonly newBranch = signal('');
  protected readonly pick = signal('');
  protected readonly pickName = signal('');
  protected readonly only = signal(false);
  protected readonly note = signal('');
  protected readonly skipped = signal('');
  protected readonly branches = signal<BranchView[]>([]);
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal(false);

  protected readonly ready = computed(() => this.mode() === 'new' || this.pick().trim() !== '');
  protected readonly where = computed(() => {
    const p = this.pick().trim();
    if (!p) return '';
    const b = this.branches().find((x) => x.name === p);
    return b ? `Exists in ${b.repos.join(', ')}` : 'No repository has this branch';
  });

  constructor() {
    // What the server knows that a form cannot: the number that is free (not
    // just one no universe has) and the branches there are.
    queueMicrotask(() => {
      const multiverse = this.multiverse();
      this.api.get<NextView>('next', { multiverse }).then((n) => {
        if (!this.name()) this.name.set(String(n.number));
        if (n.skipped?.length) this.skipped.set(`Skipped ${n.skipped.join('; ')}.`);
      }).catch(() => undefined);
      this.api.get<{ branches: BranchView[] }>('branches', { multiverse }).then((r) => this.branches.set(r.branches ?? [])).catch(() => undefined);
    });
  }

  protected val(e: Event): string { return (e.target as HTMLInputElement).value; }

  protected async submit(e: Event): Promise<void> {
    e.preventDefault();
    if (!this.ready() || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    const multiverse = this.multiverse();
    const req: OpRequest = this.mode() === 'new'
      ? { action: 'new', multiverse, name: this.name().trim(), base: this.base().trim(), branch: this.newBranch().trim(), note: this.note().trim() }
      : { action: 'checkout', multiverse, branch: this.pick().trim(), name: this.pickName().trim(), onlyExisting: this.only(), note: this.note().trim() };
    try {
      await this.ops.run(req);
      this.modal().close();
    } catch (err) {
      this.error.set((err as Error).message);
    } finally {
      this.busy.set(false);
    }
  }
}
