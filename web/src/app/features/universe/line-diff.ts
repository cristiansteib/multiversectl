export interface DiffLine { op: ' ' | '-' | '+'; text: string; oldNo?: number; newNo?: number }

const lines = (s: string): string[] => (s ? s.replace(/\n$/, '').split('\n') : []);

/**
 * Compares two texts line by line, by their longest common run of lines. A
 * manifest is a few hundred lines, so the table it builds is small.
 */
export function diffLines(oldText: string, newText: string): DiffLine[] {
  const a = lines(oldText), b = lines(newText);
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { out.push({ op: ' ', text: a[i], oldNo: i + 1, newNo: j + 1 }); i++; j++; }
    else if (lcs[i + 1][j] >= lcs[i][j + 1]) { out.push({ op: '-', text: a[i], oldNo: i + 1 }); i++; }
    else { out.push({ op: '+', text: b[j], newNo: j + 1 }); j++; }
  }
  for (; i < a.length; i++) out.push({ op: '-', text: a[i], oldNo: i + 1 });
  for (; j < b.length; j++) out.push({ op: '+', text: b[j], newNo: j + 1 });
  return out;
}
