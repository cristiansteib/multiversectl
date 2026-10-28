import { ChangeDetectionStrategy, Component, ElementRef, computed, inject, signal, viewChild } from '@angular/core';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { filter } from 'rxjs';
import { Events } from '../core/events.service';
import { Ops } from '../core/ops.service';
import { ThemeService } from '../core/theme.service';
import { Updater } from '../core/updater.service';
import { AgoPipe } from '../shared/ui/ago.pipe';
import { Icon } from '../shared/ui/icon';
import { OpsDock } from './ops-dock';
import { ToastHost } from './toast-host';
import { UpdateDialog } from './update-dialog';

interface Section { path: string; label: string; icon: string; exact?: boolean }

/** The frame: the top bar, the sections on the left, and what the section shows. */
@Component({
  selector: 'mv-shell',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, Icon, OpsDock, ToastHost, UpdateDialog, AgoPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './shell.html',
  styleUrl: './shell.css',
})
export class Shell {
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  protected readonly theme = inject(ThemeService);
  protected readonly updater = inject(Updater);
  private readonly router = inject(Router);

  protected readonly sections: Section[] = [
    { path: '/', label: 'Overview', icon: 'overview', exact: true },
    { path: '/fleet', label: 'Universes', icon: 'fleet' },
    { path: '/catalog', label: 'Catalog', icon: 'catalog' },
    { path: '/stats', label: 'Resources', icon: 'stats' },
    { path: '/system', label: 'System', icon: 'system' },
  ];
  protected readonly menu = signal(false);
  /** The sections as icons only; kept for the next visit. */
  protected readonly collapsed = signal(this.savedCollapse());

  protected toggleRail(): void {
    const next = !this.collapsed();
    this.collapsed.set(next);
    try { localStorage.setItem('rail-collapsed', next ? '1' : '0'); } catch { /* private mode */ }
  }

  private savedCollapse(): boolean {
    try { return localStorage.getItem('rail-collapsed') === '1'; } catch { return false; }
  }
  protected readonly refreshing = signal(false);
  protected readonly version = computed(() => this.events.hello()?.version ?? this.events.overview()?.version ?? '');
  protected readonly dockOpen = computed(() => this.ops.list().length > 0);

  private readonly main = viewChild.required<ElementRef<HTMLElement>>('main');

  constructor() {
    // A new screen: close the menu that led to it, and put the keyboard at its start, as a page load would.
    this.router.events.pipe(filter((e) => e instanceof NavigationEnd), takeUntilDestroyed()).subscribe(() => {
      this.menu.set(false);
      this.main().nativeElement.focus({ preventScroll: true });
    });
  }

  /** The link is to the page's own #main, which `<base href="/">` would make a visit to the overview. */
  protected skipToMain(e: Event): void {
    e.preventDefault();
    this.main().nativeElement.focus();
  }

  protected async refresh(): Promise<void> {
    this.refreshing.set(true);
    try { await this.events.refresh(); } finally { this.refreshing.set(false); }
  }
}
