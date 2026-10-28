import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map } from 'rxjs';
import { Events } from '../core/events.service';
import { Ops } from '../core/ops.service';
import { ThemeService } from '../core/theme.service';
import { AgoPipe } from '../shared/ui/ago.pipe';
import { Icon } from '../shared/ui/icon';
import { OpsDock } from './ops-dock';
import { ToastHost } from './toast-host';
import { UpdateBanner } from './update-banner';

interface Section { path: string; label: string; icon: string; exact?: boolean }

/** The frame: the top bar, the sections on the left, and what the section shows. */
@Component({
  selector: 'mv-shell',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, Icon, OpsDock, ToastHost, UpdateBanner, AgoPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './shell.html',
  styleUrl: './shell.css',
})
export class Shell {
  protected readonly events = inject(Events);
  protected readonly ops = inject(Ops);
  protected readonly theme = inject(ThemeService);
  private readonly router = inject(Router);

  protected readonly sections: Section[] = [
    { path: '/', label: 'Overview', icon: 'overview', exact: true },
    { path: '/fleet', label: 'Universes', icon: 'fleet' },
    { path: '/catalog', label: 'Catalog', icon: 'catalog' },
    { path: '/stats', label: 'Resources', icon: 'stats' },
    { path: '/system', label: 'System', icon: 'system' },
  ];
  protected readonly menu = signal(false);
  protected readonly refreshing = signal(false);
  protected readonly version = computed(() => this.events.hello()?.version ?? this.events.overview()?.version ?? '');
  protected readonly dockOpen = computed(() => this.ops.list().length > 0);

  // Closing the menu when the route changes keeps it from covering the page it led to.
  private readonly navigated = toSignal(this.router.events.pipe(filter((e) => e instanceof NavigationEnd), map(() => this.menu.set(false))));

  protected async refresh(): Promise<void> {
    this.refreshing.set(true);
    try { await this.events.refresh(); } finally { this.refreshing.set(false); }
  }
}
