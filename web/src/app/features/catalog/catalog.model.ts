import { CatalogProgress, NodeView } from '../../core/models';

/** How reading one catalog repository is going, as the server reported it. */
export interface Step { phase: CatalogProgress['phase']; detail?: string; ms?: number; repo: string }

/** Steps and nodes name a repository the same way: by address and, when there is one, ref. */
export const stepKey = (repo: string, ref?: string): string => repo + (ref ? '@' + ref : '');

export function countNodes(n: NodeView): number {
  return 1 + (n.children ?? []).reduce((a, c) => a + countNodes(c), 0);
}

export interface Tally { multiverses: number; catalogs: number; installed: number; stale: number; unavailable: number; ignored: number }

/** What the tree holds, and how much of it could not be read, counted once for the page's summary. */
export function tally(root: NodeView): Tally {
  const t: Tally = { multiverses: 0, catalogs: 0, installed: 0, stale: 0, unavailable: 0, ignored: 0 };
  const walk = (n: NodeView, top: boolean) => {
    // Left out on this machine: not part of what the catalog holds here.
    if (n.ignored) { t.ignored++; return; }
    if (n.kind === 'multiverse') { t.multiverses++; if (n.installedAs) t.installed++; }
    else if (!top) t.catalogs++;
    if (n.unavailable) t.unavailable++;
    else if (n.stale) t.stale++;
    (n.children ?? []).forEach((c) => walk(c, false));
  };
  walk(root, true);
  return t;
}
