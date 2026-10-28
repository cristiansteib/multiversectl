import { Injectable, computed, inject, signal } from '@angular/core';
import { Subject } from 'rxjs';
import { Api } from './api.service';
import { CatalogProgress, FleetView, Hello, Op, OpLine, OverviewView, UpdateNotice } from './models';

const POLL_MS = 10_000;

/**
 * A universe's own `multiverse` field is the name of its compose project
 * ("acme-u1"), not the multiverse it belongs to; every request and link needs the
 * latter, which is the entry it is listed under. Said once, here, so nothing
 * downstream has to remember it.
 */
/** An overview whose lists are lists: a machine with nothing installed may send none (`null`). */
const whole = (o: OverviewView): OverviewView => ({ ...o, status: { ...o.status, multiverses: o.status?.multiverses ?? [] } });

const named = (f: FleetView): FleetView => ({
  ...f,
  multiverses: (f.multiverses ?? []).map((m) => ({ ...m, universes: (m.universes ?? []).map((u) => ({ ...u, multiverse: m.name })) })),
});

/**
 * What the page knows about the machine, fed by the websocket. Screens read the
 * signals and are redrawn when a piece changes. While the socket is down the
 * store asks for itself every ten seconds, so the page still works behind
 * something that drops websockets.
 */
@Injectable({ providedIn: 'root' })
export class Events {
  private readonly api = inject(Api);

  readonly connected = signal(false);
  readonly hello = signal<Hello | null>(null);
  readonly readOnly = computed(() => this.hello()?.readOnly ?? this.overview()?.readOnly ?? false);
  readonly overview = signal<OverviewView | null>(null);
  readonly fleet = signal<FleetView | null>(null);
  readonly update = signal<UpdateNotice | null>(null);
  readonly updatedAt = signal<Date | null>(null);
  readonly error = signal<string | null>(null);

  readonly op$ = new Subject<Op>();
  readonly opLine$ = new Subject<{ id: string } & OpLine>();
  readonly catalogProgress$ = new Subject<CatalogProgress>();
  /** A piece of a log this page asked for over the socket, and the end of one. */
  readonly log$ = new Subject<{ id: string; text: string }>();
  readonly logEnd$ = new Subject<{ id: string; error?: string }>();

  private socket: WebSocket | null = null;
  private retries = 0;
  private poll: ReturnType<typeof setInterval> | undefined;

  start(): void {
    void this.reload();
    this.connect();
  }

  /** Ask for both again; `fetch` also contacts the remotes. */
  async reload(fetch = false): Promise<boolean> {
    try {
      const [overview, fleet] = await Promise.all([
        this.api.get<OverviewView>('overview', { fetch }),
        this.api.get<FleetView>('fleet'),
      ]);
      this.overview.set(whole(overview));
      this.fleet.set(named(fleet));
      this.updatedAt.set(new Date());
      this.error.set(null);
      return true;
    } catch (e) {
      this.error.set((e as Error).message);
      return false;
    }
  }

  /** Ask the server to look again now, and read what it finds. */
  async refresh(): Promise<void> {
    if (this.connected()) this.socket?.send(JSON.stringify({ type: 'refresh' }));
    await this.reload();
  }

  /** Say something to the server over the socket; false when it is not open (the caller then asks over HTTP). */
  send(message: object): boolean {
    if (this.socket?.readyState !== WebSocket.OPEN) return false;
    this.socket.send(JSON.stringify(message));
    return true;
  }

  private connect(): void {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = new WebSocket(`${proto}//${location.host}/ws`);
    this.socket = socket;
    socket.onopen = () => { this.retries = 0; this.live(true); };
    socket.onmessage = (m) => {
      try { this.onEvent(JSON.parse(m.data as string) as { type: string; data: never }); } catch { /* not ours */ }
    };
    socket.onclose = () => {
      this.live(false);
      setTimeout(() => this.connect(), Math.min(10_000, 1000 * 2 ** this.retries++));
    };
  }

  private live(on: boolean): void {
    this.connected.set(on);
    clearInterval(this.poll);
    if (!on) this.poll = setInterval(() => { if (!document.hidden) void this.reload(); }, POLL_MS);
  }

  private onEvent(e: { type: string; data: never }): void {
    switch (e.type) {
      case 'hello': {
        // The server came back as a newer build: the page in front of it is the old one.
        const hello = e.data as Hello;
        const before = this.hello()?.version;
        if (before && hello.version && before !== hello.version) { location.reload(); return; }
        this.hello.set(hello);
        break;
      }
      case 'fleet': this.fleet.set(named(e.data)); this.updatedAt.set(new Date()); break;
      case 'overview': this.overview.set(whole(e.data)); this.updatedAt.set(new Date()); break;
      case 'update': this.update.set((e.data as { notice?: UpdateNotice }).notice ?? null); break;
      case 'catalogProgress': this.catalogProgress$.next(e.data); break;
      case 'op': this.op$.next(e.data); break;
      case 'log': this.log$.next(e.data); break;
      case 'logEnd': { const end = e.data as { id: string; error?: string }; this.logEnd$.next({ id: end.id, error: end.error || undefined }); break; }
      case 'opline': this.opLine$.next(e.data); break;
    }
  }
}
