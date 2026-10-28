import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, input, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { MultiverseView, NodeView, RepoView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { Icon } from '../../shared/ui/icon';
import { Place } from '../../shared/ui/place';
import { RepoChip } from '../../shared/ui/repo-chip';
import { plural } from '../../core/util';
import { FetchProgress } from './fetch-progress';
import { Step, countNodes, stepKey } from './catalog.model';

interface Row { node: NodeView; depth: number; expandable: boolean; open: boolean; root: boolean; parent: string | null; step?: Step }

// A catalog this small is opened on arrival, so the multiverses are in view;
// a large one stays closed and the filter opens what it finds.
const OPEN_BELOW = 150;

/**
 * The catalog as a map: one row per node, indented under its parent, with the
 * arrow keys to move through it. It is a flat list of rows, not nested boxes,
 * so a long catalog stays calm to look at and quick to scan.
 */
@Component({
  selector: 'mv-catalog-tree',
  imports: [RouterLink, Badge, CopyButton, EmptyState, Icon, Place, RepoChip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './catalog-tree.html',
  styleUrl: './catalog-tree.css',
})
export class CatalogTree {
  readonly root = input.required<NodeView>();
  readonly filter = input('');
  readonly steps = input<ReadonlyMap<string, Step>>(new Map());
  readonly reading = input(false);
  /** Leave a node out on this machine, or bring it back. */
  readonly ignoreChange = output<{ node: NodeView; ignore: boolean }>();
  protected readonly events = inject(Events);
  private readonly api = inject(Api);
  /** The repositories of the installed multiverses that have been opened, by the name they are installed as. */
  private readonly repos = signal<ReadonlyMap<string, RepoView[] | null>>(new Map());

  private readonly el = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly open = signal<ReadonlySet<string>>(new Set(['']));
  private readonly focused = signal('');
  private seeded = false;

  constructor() {
    // What an operation did (an update clones what the manifest newly lists) is
    // not what was read before: the repositories of the open rows are read again.
    inject(Ops).finished$.pipe(takeUntilDestroyed()).subscribe(() => {
      const names = this.rows().filter((r) => r.open && r.node.kind === 'multiverse' && r.node.installedAs).map((r) => r.node.installedAs as string);
      this.repos.set(new Map());
      names.forEach((n) => void this.loadRepos(n));
    });
    effect(() => {
      const root = this.root();
      if (this.seeded) return;
      this.seeded = true;
      if (countNodes(root) <= OPEN_BELOW) this.setAll(true);
    });
  }

  protected readonly rows = computed<Row[]>(() => {
    const f = this.filter().trim().toLowerCase();
    const matches = (n: NodeView) => [n.name, n.path, n.repo ?? '', n.loc?.short ?? '', n.within ?? ''].some((s) => s.toLowerCase().includes(f));
    // With a filter, what matches is shown with everything under it and the
    // way down to it; without one, every node is.
    const kept = new Set<string>();
    const keep = (n: NodeView, inherited: boolean): boolean => {
      const mine = matches(n);
      const below = (n.children ?? []).map((c) => keep(c, !!f && (inherited || mine)));
      const any = !f || inherited || mine || below.some(Boolean);
      if (any) kept.add(n.path);
      return any;
    };
    keep(this.root(), false);

    const open = this.open();
    const steps = this.steps();
    const out: Row[] = [];
    const walk = (n: NodeView, depth: number, parent: string | null) => {
      if (!kept.has(n.path)) return;
      // A catalog opens to its children; a multiverse opens to what is known of it.
      const expandable = n.kind === 'multiverse' || !n.ignored;
      const isOpen = expandable && (n.kind === 'catalog' ? !!f || open.has(n.path) : open.has(n.path));
      out.push({ node: n, depth, expandable, open: isOpen, root: depth === 0, parent, step: n.kind === 'catalog' && !n.ignored && n.repo ? steps.get(stepKey(n.repo, n.ref)) : undefined });
      if (isOpen) (n.children ?? []).forEach((c) => walk(c, depth + 1, n.path));
    };
    walk(this.root(), 0, null);
    return out;
  });

  protected readonly tabStop = computed(() => {
    const rows = this.rows();
    return rows.some((r) => r.node.path === this.focused()) ? this.focused() : (rows[0]?.node.path ?? '');
  });

  expandAll(): void { this.setAll(true); }
  collapseAll(): void { this.setAll(false); }

  private setAll(on: boolean): void {
    const next = new Set<string>(['']);
    const walk = (n: NodeView) => {
      if (n.kind !== 'catalog') return;
      if (on) next.add(n.path);
      (n.children ?? []).forEach(walk);
    };
    walk(this.root());
    if (!on) next.delete('');
    this.open.set(on ? next : new Set(['']));
  }

  protected toggle(r: Row): void {
    if (!r.expandable || (this.filter() && r.node.kind === 'catalog')) return;
    const next = new Set(this.open());
    if (next.has(r.node.path)) next.delete(r.node.path); else next.add(r.node.path);
    this.open.set(next);
    if (r.node.kind === 'multiverse' && r.node.installedAs && next.has(r.node.path)) void this.loadRepos(r.node.installedAs);
  }

  protected reposOf(installedAs: string): RepoView[] | null | undefined { return this.repos().get(installedAs); }
  protected retryRepos(name: string): void { const next = new Map(this.repos()); next.delete(name); this.repos.set(next); void this.loadRepos(name); }

  /** What its manifest lists is only known once it is installed: it is read from the multiverse itself. */
  private async loadRepos(name: string): Promise<void> {
    if (this.repos().get(name)) return;
    try {
      const view = await this.api.get<MultiverseView>(`multiverses/${encodeURIComponent(name)}`);
      this.repos.set(new Map(this.repos()).set(name, view.repos));
    } catch {
      // Said, with a way to try again; not remembered as "none".
      this.repos.set(new Map(this.repos()).set(name, null));
    }
  }

  /** A press on a row opens it; a press on a link or button inside it does what that says. */
  protected press(e: MouseEvent, r: Row): void {
    this.focused.set(r.node.path);
    if ((e.target as HTMLElement).closest('a, button')) return;
    this.toggle(r);
  }

  /** How many universes an installed multiverse has, and how many of them run. */
  protected universesOf(name: string): string {
    const m = this.events.fleet()?.multiverses.find((x) => x.name === name);
    if (!m) return '';
    const running = m.universes.filter((u) => u.running).length;
    return m.universes.length ? `${plural(m.universes.length, 'universe')}, ${running} running` : 'no universes yet';
  }

  protected kindLabel(r: Row): string { return r.root ? 'root catalog' : r.node.kind; }
  protected canIgnore(r: Row): boolean { return !r.root && !this.events.readOnly(); }
  protected icon(r: Row): string { return r.node.kind === 'multiverse' ? 'multiverse' : 'catalog'; }

  protected key(e: KeyboardEvent, r: Row, i: number): void {
    if (e.target !== e.currentTarget || e.altKey || e.ctrlKey || e.metaKey) return;
    const rows = this.rows();
    const go = (to: number) => { e.preventDefault(); this.focusRow(rows[Math.max(0, Math.min(rows.length - 1, to))]); };
    switch (e.key) {
      case 'ArrowDown': go(i + 1); break;
      case 'ArrowUp': go(i - 1); break;
      case 'Home': go(0); break;
      case 'End': go(rows.length - 1); break;
      case 'ArrowRight':
        e.preventDefault();
        if (r.expandable && !r.open) this.toggle(r); else if (r.open) go(i + 1);
        break;
      case 'ArrowLeft':
        e.preventDefault();
        if (r.open && !this.filter()) this.toggle(r);
        else if (r.parent !== null) go(rows.findIndex((x) => x.node.path === r.parent));
        break;
      case 'Enter': case ' ':
        e.preventDefault();
        this.toggle(r);
        break;
    }
  }

  private focusRow(r: Row | undefined): void {
    if (!r) return;
    this.focused.set(r.node.path);
    queueMicrotask(() => {
      const items = this.el.nativeElement.querySelectorAll<HTMLElement>('[role="treeitem"]');
      for (const it of Array.from(items)) if (it.dataset['path'] === r.node.path) { it.focus(); return; }
    });
  }
}
