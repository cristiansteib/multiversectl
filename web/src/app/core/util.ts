import { signal } from '@angular/core';
import { DiffView, LocView } from './models';

/** A repository as a short name: owner/repo from a URL. */
export const shortRepoName = (u: string): string =>
  u.replace(/^[a-z+]+:\/\//, '').replace(/^[^@/]*@/, '').replace(/:/, '/').replace(/\.git$/, '');

/**
 * How much a repository has changed: what its branch amounts to against the
 * branch it will be merged into, and what is edited and not committed yet. It is
 * what puts the work first where repositories are listed.
 */
export const changeSize = (row?: { change?: DiffView; uncommitted?: DiffView } | null): number =>
  (row?.change ? row.change.added + row.change.removed : 0) + (row?.uncommitted ? row.uncommitted.added + row.uncommitted.removed : 0);

/** Counting out loud, in the one place that knows the rule. */
export const plural = (n: number, one: string, many = one + 's'): string => `${n} ${n === 1 ? one : many}`;

/**
 * What relative times are measured from. It moves every half minute, and
 * `ago` reads it, so a time on a page left open keeps counting without each
 * place that shows one having to ask.
 */
export const clock = signal(Date.now());
if (typeof setInterval === 'function') setInterval(() => clock.set(Date.now()), 30_000);

export function ago(iso: string | undefined, now = clock()): string {
  if (!iso) return '';
  const s = (now - Date.parse(iso)) / 1000;
  if (!(s >= 90)) return 'just now';
  for (const [limit, name] of [[86400 * 30, 'mo'], [86400, 'd'], [3600, 'h'], [60, 'min']] as const) {
    if (s >= limit) return `${Math.floor(s / limit)} ${name} ago`;
  }
  return 'just now';
}

/** The universe's address on the page: the same everywhere it is linked. */
export const universeLink = (multiverse: string, universe: string): string[] => ['/universe', multiverse, universe];

/** The repository's page on the web, when it has one: a checkout on disk does not. */
export function webUrlOf(loc: LocView | null | undefined): string | null {
  if (!loc || loc.platform === 'file' || !loc.path) return null;
  const host = loc.platform === 'github' ? 'github.com' : loc.platform === 'gitlab' ? (loc.host ?? 'gitlab.com') : loc.host;
  if (!host) return null;
  return `https://${host}/${loc.path.replace(/^\/+/, '').replace(/\.git$/, '')}`;
}

/** A branch's page on the repository's web page; GitLab keeps its pages under /-/. */
export function branchUrlOf(loc: LocView | null | undefined, branch: string): string | null {
  const base = webUrlOf(loc);
  if (!base || !branch) return null;
  const path = branch.split('/').map(encodeURIComponent).join('/');
  return `${base}${loc?.platform === 'gitlab' ? '/-' : ''}/tree/${path}`;
}

/** Only a web address becomes a link: the field is free text that anyone with a shell wrote. */
export const isHttpUrl = (s: string | undefined | null): s is string => !!s && /^https?:\/\//.test(s);

/**
 * Put text on the clipboard. It is raced against a second of patience: a browser
 * that will not write does not always refuse, and a button that waits for ever
 * is a button that does nothing when pressed.
 */
export function copyText(text: string): Promise<boolean> {
  const clip = navigator.clipboard;
  if (!clip) return Promise.resolve(false);
  return Promise.race([
    clip.writeText(text).then(() => true, () => false),
    new Promise<boolean>((say) => setTimeout(() => say(false), 1000)),
  ]);
}
