import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { Subject } from 'rxjs';
import { Api } from './api.service';
import { Events } from './events.service';
import { Op, OpRequest } from './models';
import { Toasts } from './toast.service';

const KEEP = 30;
const MAX_LINES = 3000;

/**
 * Everything the page can do is a use case the command line has too. The server
 * runs it in the background and sends its output as it happens; the dock at the
 * foot of the page is where that is read.
 */
@Injectable({ providedIn: 'root' })
export class Ops {
  private readonly api = inject(Api);
  private readonly events = inject(Events);
  private readonly toasts = inject(Toasts);

  readonly list = signal<Op[]>([]);
  readonly shownId = signal<string | null>(null);
  readonly open = signal(false);
  readonly shown = computed(() => this.list().find((o) => o.id === this.shownId()) ?? this.list().at(-1) ?? null);
  readonly running = computed(() => this.list().filter((o) => o.state === 'running').length);
  /** Fires when an operation that was running has ended, so a screen can read again. */
  readonly finished$ = new Subject<Op>();

  constructor() {
    this.events.op$.subscribe((op) => this.onOp(op));
    this.events.opLine$.subscribe((l) => this.onLine(l));
    // The first message of a connection carries the operations already known.
    effect(() => {
      const hello = this.events.hello();
      if (hello) untracked(() => this.hydrate(hello.ops));
    });
  }

  /** Whether something is already running in this multiverse: only one thing at a time is. */
  busy(multiverse: string): boolean {
    return this.list().some((o) => o.multiverse === multiverse && o.state === 'running');
  }

  /** Start an action and open the dock on it. A refusal comes back to the caller, which can say it where the person is looking. */
  async run(request: OpRequest): Promise<Op> {
    const op = await this.api.post<Op>('ops', request);
    // The websocket may have told the page how it went before this answer, which
    // is how it began: what is known already is not replaced by it.
    if (!this.list().some((o) => o.id === op.id)) this.upsert(op);
    this.shownId.set(op.id);
    this.open.set(true);
    return op;
  }

  /** Like run, for a button with no dialog to say what went wrong. */
  async runOrSay(request: OpRequest): Promise<void> {
    try { await this.run(request); } catch (e) { this.toasts.show((e as Error).message, 'bad'); }
  }

  show(id: string): void { this.shownId.set(id); }
  toggle(): void { this.open.update((o) => !o); }

  hydrate(ops: Op[]): void { this.list.set(ops.map((o) => ({ ...o, lines: o.lines ?? [] }))); }

  private upsert(op: Op): void {
    this.list.update((list) => {
      const i = list.findIndex((o) => o.id === op.id);
      const next = list.slice();
      if (i < 0) next.push({ ...op, lines: op.lines ?? [] });
      else next[i] = { ...op, lines: op.lines?.length ? op.lines : list[i].lines };
      return next.slice(-KEEP);
    });
  }

  private onOp(op: Op): void {
    const before = this.list().find((o) => o.id === op.id);
    this.upsert(op);
    if (!this.shownId() || !before) this.shownId.set(op.id);
    if (!before && op.state === 'running') this.open.set(true);
    if (before?.state === 'running' && op.state !== 'running') {
      this.toasts.show(`${op.title}: ${op.state === 'done' ? 'done' : 'failed'}`, op.state === 'done' ? 'ok' : 'bad');
      this.finished$.next(op);
    }
  }

  private onLine(l: { id: string; kind: Op['lines'][number]['kind']; text: string }): void {
    this.list.update((list) => list.map((o) => {
      if (o.id !== l.id) return o;
      const lines = [...o.lines, { kind: l.kind, text: l.text }];
      return { ...o, lines: lines.length > MAX_LINES ? lines.slice(-MAX_LINES) : lines };
    }));
  }
}
