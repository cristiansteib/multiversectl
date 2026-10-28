import { UniverseView } from '../../core/models';

/** What a universe is doing, in the three words the page uses for it. */
export type RunState = 'running' | 'partial' | 'stopped';

/** How many containers its apps expect to be up. */
export const expectedOf = (u: UniverseView): number => u.apps.reduce((n, a) => n + a.expected, 0);

export function runState(u: UniverseView): RunState {
  if (!u.running) return 'stopped';
  const total = expectedOf(u);
  return total && u.running < total ? 'partial' : 'running';
}

export const dirtyCheckouts = (u: UniverseView): string[] => u.checkouts.filter((c) => c.dirty).map((c) => c.repo);

/** Why a person might want to look at it: only what they can act on. */
export function reasonsFor(u: UniverseView): string[] {
  const out: string[] = [];
  if (runState(u) === 'partial') out.push(`only ${u.running} of ${expectedOf(u)} containers are up`);
  if (u.behind) out.push(`${u.behind} ${u.behind === 1 ? 'commit' : 'commits'} behind the branches it follows`);
  const dirty = dirtyCheckouts(u);
  if (dirty.length) out.push(`uncommitted changes in ${dirty.join(', ')}`);
  return out;
}

/** Numbers by value, then names: 2 before 10, and 10 before "billing". */
export const byName = (a: UniverseView, b: UniverseView): number => {
  const na = /^\d+$/.test(a.name), nb = /^\d+$/.test(b.name);
  if (na !== nb) return na ? -1 : 1;
  return a.name.localeCompare(b.name, undefined, { numeric: true });
};

/** Only a web address becomes a link: the field is free text that anyone with a shell wrote. */
export const isWebUrl = (s: string | undefined): boolean => !!s && /^https?:\/\//.test(s);
