import { UpdateNotice } from './models';

/** "6 fixes since": what the release brings, as far as is known (a plus says there is more). */
export function brings(n: UpdateNotice): string {
  const count = (k: number, one: string, many: string) => `${k}${n.more ? '+' : ''} ${k === 1 && !n.more ? one : many}`;
  return n.fixes ? count(n.fixes, 'fix', 'fixes') : n.changes ? count(n.changes, 'change', 'changes') : '';
}

/** What is left over after the highlights shown. */
export function rest(n: UpdateNotice): string | null {
  const more = n.fixes - (n.highlights?.length ?? 0);
  return more > 0 ? `and ${more}${n.more ? '+' : ''} more` : n.more ? 'and older ones' : null;
}
