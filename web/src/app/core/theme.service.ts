import { Injectable, signal } from '@angular/core';

export type Theme = 'light' | 'dark';

/** The colour theme: the person's choice when they made one, else the system's. */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  readonly theme = signal<Theme>(this.initial());

  constructor() {
    this.apply(this.theme());
    // Until a choice is made the theme is the system's, and follows it.
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
      if (this.chosen()) return;
      const next: Theme = e.matches ? 'dark' : 'light';
      this.theme.set(next);
      this.apply(next);
    });
  }

  private chosen(): boolean {
    try { return localStorage.getItem('theme') !== null; } catch { return false; }
  }

  toggle(): void {
    const next: Theme = this.theme() === 'dark' ? 'light' : 'dark';
    this.theme.set(next);
    this.apply(next);
    try { localStorage.setItem('theme', next); } catch { /* private mode */ }
  }

  private initial(): Theme {
    try {
      const saved = localStorage.getItem('theme');
      if (saved === 'light' || saved === 'dark') return saved;
    } catch { /* private mode */ }
    return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  private apply(t: Theme): void { document.documentElement.dataset['theme'] = t; }
}
