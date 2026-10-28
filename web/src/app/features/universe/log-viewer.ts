import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, afterRenderEffect, computed, effect, inject, input, model, signal, untracked, viewChild } from '@angular/core';
import { Toasts } from '../../core/toast.service';
import { plural } from '../../core/util';
import { EmptyState } from '../../shared/ui/empty-state';
import { Icon } from '../../shared/ui/icon';
import { LogLine, Run, parseLogLine } from './ansi';

const MAX_LINES = 3000;
const TAILS = [100, 200, 500, 1000, 2000];
type State = 'idle' | 'loading' | 'following' | 'loaded' | 'ended' | 'failed';
const STATE: Record<State, [string, string]> = {
  idle: ['', ''], loading: ['', 'opening…'], following: ['ok', 'following'], loaded: ['ok', 'last lines'], ended: ['', 'ended'], failed: ['warn', 'stopped'],
};

interface Seg { s: string; cls: string; mark: boolean }
interface Row { id: number; at: number; level: string; segs: Seg[] }

/** The filter is marked inside each coloured run of the line, not across it: a match that straddles a colour change would have to be split up to be drawn anyway. */
function segsOf(parts: Run[], needle: string): Seg[] {
  if (!needle) return parts.map((p) => ({ s: p.s, cls: p.cls, mark: false }));
  const out: Seg[] = [];
  const low = needle.toLowerCase();
  for (const p of parts) {
    const text = p.s.toLowerCase();
    let i = 0;
    for (;;) {
      const at = text.indexOf(low, i);
      if (at < 0) { if (i < p.s.length) out.push({ s: p.s.slice(i), cls: p.cls, mark: false }); break; }
      if (at > i) out.push({ s: p.s.slice(i, at), cls: p.cls, mark: false });
      out.push({ s: p.s.slice(at, at + needle.length), cls: p.cls, mark: true });
      i = at + needle.length;
    }
  }
  return out;
}

/**
 * A service's log, read from docker over a stream. The stream runs only while a
 * service is picked and the page is on screen, and is closed when this goes
 * away: each open log is a docker process on the server.
 */
@Component({
  selector: 'mv-log-viewer',
  imports: [EmptyState, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './log-viewer.html',
  styleUrl: './log-viewer.css',
})
export class LogViewer {
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly choices = input.required<{ key: string; running: boolean }[]>();
  /** The service being read, as "app/service". */
  readonly selected = model('');

  private readonly toasts = inject(Toasts);
  private readonly box = viewChild<ElementRef<HTMLElement>>('box');

  protected readonly tails = TAILS;
  protected readonly tail = signal(200);
  protected readonly follow = signal(true);
  protected readonly wrap = signal(true);
  protected readonly times = signal(true);
  protected readonly filter = signal('');
  protected readonly state = signal<State>('idle');
  protected readonly detail = signal('');
  protected readonly lines = signal<LogLine[]>([]);
  /** Whether the view is at the newest line: only then does new output scroll it. */
  protected readonly atEnd = signal(true);

  protected readonly stateLabel = computed(() => STATE[this.state()]);
  protected readonly rows = computed<Row[]>(() => {
    const needle = this.filter().trim();
    const low = needle.toLowerCase();
    const out: Row[] = [];
    for (const l of this.lines()) {
      if (low && !l.text.toLowerCase().includes(low)) continue;
      out.push({ id: l.id, at: l.at, level: l.level, segs: segsOf(l.parts, needle) });
    }
    return out;
  });
  protected readonly count = computed(() => (this.filter().trim() ? `${this.rows().length} of ${this.lines().length} lines` : plural(this.rows().length, 'line')));

  private abort: AbortController | null = null;
  private buffer: LogLine[] = [];
  private flushing: ReturnType<typeof setTimeout> | undefined;
  private nextId = 0;

  constructor() {
    effect(() => {
      const key = this.selected();
      const tail = this.tail();
      const follow = this.follow();
      const mv = this.multiverse();
      const u = this.universe();
      untracked(() => {
        if (key && mv && u) void this.start(mv, u, key, tail, follow);
        else this.stop();
      });
    });
    // Follow the output unless the reader has scrolled up to read.
    afterRenderEffect(() => {
      this.rows();
      const el = this.box()?.nativeElement;
      if (el && untracked(this.atEnd)) el.scrollTop = el.scrollHeight;
    });
    inject(DestroyRef).onDestroy(() => { this.stop(); clearTimeout(this.flushing); });
  }

  protected pick(e: Event): void { this.selected.set((e.target as HTMLSelectElement).value); }
  protected setTail(e: Event): void { this.tail.set(Number((e.target as HTMLSelectElement).value)); }
  protected time = (at: number): string => new Date(at).toLocaleTimeString(undefined, { hour12: false });

  protected scrolled(): void {
    const el = this.box()?.nativeElement;
    if (el) this.atEnd.set(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
  }
  protected latest(): void {
    const el = this.box()?.nativeElement;
    if (el) { el.scrollTop = el.scrollHeight; this.atEnd.set(true); }
  }
  protected clear(): void { this.lines.set([]); }

  protected async copy(): Promise<void> {
    const n = this.rows().length;
    const ok = await Promise.race([
      navigator.clipboard?.writeText(this.asText()).then(() => true, () => false) ?? Promise.resolve(false),
      new Promise<boolean>((say) => setTimeout(() => say(false), 1000)),
    ]);
    this.toasts.show(ok ? `Copied ${plural(n, 'line')}` : 'Copy failed: select the lines and press ctrl-C', ok ? 'ok' : 'bad');
  }

  protected download(): void {
    const url = URL.createObjectURL(new Blob([this.asText()], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = `${this.multiverse()}-${this.universe()}-${this.selected().replace('/', '-')}.log`;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }

  private asText(): string {
    const needle = this.filter().trim().toLowerCase();
    return this.lines()
      .filter((l) => !needle || l.text.toLowerCase().includes(needle))
      .map((l) => (this.times() && l.at ? new Date(l.at).toISOString() + ' ' : '') + l.text)
      .join('\n');
  }

  private stop(): void {
    this.abort?.abort();
    this.abort = null;
    this.buffer = [];
    this.state.set('idle');
    this.lines.set([]);
  }

  private async start(multiverse: string, universe: string, key: string, tail: number, follow: boolean): Promise<void> {
    this.abort?.abort();
    const abort = new AbortController();
    this.abort = abort;
    this.buffer = [];
    this.lines.set([]);
    this.atEnd.set(true);
    this.state.set('loading');
    this.detail.set('');
    const slash = key.indexOf('/');
    const q = new URLSearchParams({ multiverse, universe, app: key.slice(0, slash), service: key.slice(slash + 1), tail: String(tail), follow: follow ? '1' : '0' });
    try {
      const res = await fetch(`/api/universe/logs?${q}`, { signal: abort.signal });
      if (!res.ok) {
        let body: { error?: string } = {};
        try { body = await res.json(); } catch { /* empty */ }
        throw new Error(body.error || res.statusText);
      }
      this.state.set(follow ? 'following' : 'loaded');
      const reader = res.body!.getReader();
      const decoder = new TextDecoder();
      let carry = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        const parts = (carry + decoder.decode(value, { stream: true })).split('\n');
        carry = parts.pop() ?? '';
        if (parts.length) this.queue(parts);
      }
      if (carry) this.queue([carry]);
      if (this.abort === abort) this.state.set('ended');
    } catch (e) {
      if ((e as Error).name === 'AbortError') return;
      this.state.set('failed');
      this.detail.set((e as Error).message);
    }
  }

  /** Lines arrive in bursts; they are added once a frame, so a busy log does not rebuild the view per chunk. */
  private queue(raw: string[]): void {
    for (const line of raw) this.buffer.push(parseLogLine(this.nextId++, line));
    if (this.flushing === undefined) this.flushing = setTimeout(() => this.flush(), 60);
  }
  private flush(): void {
    this.flushing = undefined;
    const add = this.buffer;
    this.buffer = [];
    this.lines.update((l) => {
      const next = l.concat(add);
      return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
    });
  }
}
