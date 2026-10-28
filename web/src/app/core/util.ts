/** A repository as a short name: owner/repo from a URL. */
export const shortRepoName = (u: string): string =>
  u.replace(/^[a-z+]+:\/\//, '').replace(/^[^@/]*@/, '').replace(/:/, '/').replace(/\.git$/, '');

/** Counting out loud, in the one place that knows the rule. */
export const plural = (n: number, one: string, many = one + 's'): string => `${n} ${n === 1 ? one : many}`;

export function ago(iso: string | undefined, now = Date.now()): string {
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
