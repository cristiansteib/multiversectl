import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, input, linkedSignal, viewChild } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Events } from '../../core/events.service';
import { FleetMultiverse, UniverseView } from '../../core/models';
import { plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Skeleton } from '../../shared/ui/skeleton';
import { DoctorButton, DoctorResult } from './doctor';
import { MultiverseActions } from './multiverse-actions';
import { UniverseCard } from './universe-card';
import { byName, reasonsFor, runState } from './universe-state';

export type StateFilter = 'all' | 'running' | 'stopped' | 'attention';
const STATES: { value: StateFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'attention', label: 'Needs a look' },
];

/** Every universe of every installed multiverse, and what is running in it. */
@Component({
  selector: 'mv-fleet-page',
  imports: [RouterLink, UniverseCard, MultiverseActions, DoctorButton, DoctorResult, Badge, CopyButton, EmptyState, ErrorBox, Icon, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './fleet-page.html',
  styleUrl: './fleet-page.css',
})
export class FleetPage {
  protected readonly events = inject(Events);
  private readonly router = inject(Router);

  // The filters live in the address, so a view can be sent to someone and survives a reload.
  readonly q = input<string>();
  readonly mv = input<string>();
  readonly state = input<string>();

  protected readonly states = STATES;
  protected readonly search = viewChild<ElementRef<HTMLInputElement>>('search');
  protected readonly text = linkedSignal(() => this.q() ?? '');
  protected readonly stateFilter = computed<StateFilter>(() => (STATES.some((s) => s.value === this.state()) ? (this.state() as StateFilter) : 'all'));
  protected readonly only = computed(() => this.mv() ?? '');

  protected readonly fleet = this.events.fleet;
  protected readonly names = computed(() => (this.fleet()?.multiverses ?? []).map((w) => w.name));
  protected readonly total = computed(() => (this.fleet()?.multiverses ?? []).reduce((n, w) => n + w.universes.length, 0));
  protected readonly filtering = computed(() => !!(this.text().trim() || this.only() || this.stateFilter() !== 'all'));

  /** Each multiverse with the universes that pass the filters, in order. */
  protected readonly groups = computed(() => {
    const f = this.fleet();
    if (!f) return [];
    const text = this.text().trim().toLowerCase();
    const state = this.stateFilter();
    const wanted = (u: UniverseView): boolean => {
      if (state === 'running' && runState(u) === 'stopped') return false;
      if (state === 'stopped' && runState(u) !== 'stopped') return false;
      if (state === 'attention' && !reasonsFor(u).length) return false;
      return !text || [u.name, u.note, u.pr, ...Object.values(u.branches ?? {})].some((s) => (s ?? '').toLowerCase().includes(text));
    };
    return f.multiverses
      .filter((w) => !this.only() || w.name === this.only())
      .map((w): { w: FleetMultiverse; list: UniverseView[] } => ({ w, list: w.universes.filter(wanted).sort(byName) }));
  });
  protected readonly shown = computed(() => this.groups().reduce((n, g) => n + g.list.length, 0));

  protected plural = plural;

  protected setText(value: string): void {
    this.text.set(value);
    this.go({ q: value.trim() || null });
  }
  protected setState(value: StateFilter): void { this.go({ state: value === 'all' ? null : value }); }
  protected setMultiverse(value: string): void { this.go({ mv: value || null }); }
  protected clear(): void { this.text.set(''); this.go({ q: null, mv: null, state: null }); }

  private go(params: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams: params, queryParamsHandling: 'merge', replaceUrl: true });
  }

  // "/" jumps to the search box, as it does on the sites people already use.
  @HostListener('document:keydown', ['$event'])
  protected onKey(e: KeyboardEvent): void {
    const el = e.target as HTMLElement;
    if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || /^(input|textarea|select)$/i.test(el.tagName) || el.isContentEditable) return;
    e.preventDefault();
    this.search()?.nativeElement.focus();
  }
}
