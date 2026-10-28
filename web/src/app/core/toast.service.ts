import { Injectable, signal } from '@angular/core';

export interface Toast { id: number; text: string; kind: 'info' | 'ok' | 'bad' }

/** Short messages about what just happened, at the foot of the page. */
@Injectable({ providedIn: 'root' })
export class Toasts {
  readonly list = signal<Toast[]>([]);
  private next = 1;

  show(text: string, kind: Toast['kind'] = 'info', ms = 3200): void {
    const id = this.next++;
    this.list.update((l) => [...l.slice(-3), { id, text, kind }]);
    setTimeout(() => this.dismiss(id), ms);
  }
  dismiss(id: number): void { this.list.update((l) => l.filter((t) => t.id !== id)); }
}
