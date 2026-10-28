// docker hands a log over exactly as the program wrote it, escape codes and
// all. A page that prints what it wrote shows "[32m" where the terminal showed
// green, and a spinner as every frame it ever drew.
//
// So a line is built the way a terminal builds one: what the program painted it
// in is kept, as classes rather than styles because the page's content security
// policy allows no style of its own, and what moves the cursor, erases or
// rewrites is carried out. A spinner then leaves the word it finished on, which
// is the only one that was ever meant to be read.

export interface Run { s: string; cls: string }
export interface Parsed { text: string; parts: Run[] }

// A control sequence's parameters and its final byte; after it, an operating
// system command ended by a bell or a string terminator, and last the two-byte escapes.
// eslint-disable-next-line no-control-regex
const ANSI = /\x1b(?:\[([0-9;:?<=>]*)[ -/]*([@-~])|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[@-Z\\-_])/g;
const FG = ['ac0', 'ac1', 'ac2', 'ac3', 'ac4', 'ac5', 'ac6', 'ac7'];
const TAB = 8;
// The furthest a program may send the cursor along a line it is writing.
const MAX_COLS = 4096;

interface Pen { fg: string; bold: boolean; dim: boolean; italic: boolean; under: boolean }
interface Cell { ch: string; cls: string }

export function parseAnsi(raw: string): Parsed {
  const on: Pen = { fg: '', bold: false, dim: false, italic: false, under: false };
  const cls = () => [on.fg, on.bold && 'ab', on.dim && 'ad', on.italic && 'ai', on.under && 'au'].filter(Boolean).join(' ');
  // One slot per column, so writing at a column that was written before replaces
  // what was there, as a terminal would. Writing is not capped, but moving the
  // cursor is: every column jumped over is a blank that has to be made and read back.
  const cells: (Cell | undefined)[] = [];
  let col = 0;
  const put = (ch: string) => { cells[col] = { ch, cls: cls() }; col++; };
  const plain = (run: string) => {
    for (const ch of run) {
      if (ch === '\r') col = 0;
      else if (ch === '\b') col = Math.max(0, col - 1);
      else if (ch === '\t') { do { put(' '); } while (col % TAB); }
      else if (ch >= ' ' || ch === ' ') put(ch);
    }
  };

  let at = 0;
  let m: RegExpExecArray | null;
  ANSI.lastIndex = 0;
  while ((m = ANSI.exec(raw))) {
    if (m.index > at) plain(raw.slice(at, m.index));
    col = move(on, cells, col, m[1], m[2]);
    at = ANSI.lastIndex;
  }
  if (at < raw.length) {
    // An escape the line ends in the middle of has nothing to act on.
    const tail = raw.slice(at);
    const half = tail.indexOf('\u001b');
    plain(half < 0 ? tail : tail.slice(0, half));
  }

  // Trailing blanks are what erasing a longer line left behind. Counted backwards
  // over the columns: a regular expression over the whitespace at the end of a
  // line is quadratic, and a log line can carry a great deal of it.
  let end = cells.length;
  while (end > 0 && (!cells[end - 1] || cells[end - 1]?.ch === ' ')) end--;
  const parts: Run[] = [];
  let text = '';
  for (let i = 0; i < end; i++) {
    const c = cells[i] ?? { ch: ' ', cls: '' };
    const last = parts[parts.length - 1];
    if (last && last.cls === c.cls) last.s += c.ch;
    else parts.push({ s: c.ch, cls: c.cls });
    text += c.ch;
  }
  return { text, parts };
}

// What one control sequence does, as far as a log line is concerned: colour, the
// cursor along its one line, and erasing. Anything that would need more than a
// line is left out rather than half done.
function move(on: Pen, cells: (Cell | undefined)[], col: number, params: string | undefined, final: string | undefined): number {
  const split = (params ?? '').split(';');
  const n = (i: number, or: number) => { const v = Number(split[i]); return Number.isFinite(v) && split[i] !== '' ? v : or; };
  switch (final) {
    case 'm': paint(on, params); return col;
    case 'G': return Math.min(MAX_COLS, Math.max(0, n(0, 1) - 1));
    // A move of no columns is a move of one, which is what a terminal does with a parameter of zero.
    case 'C': return Math.min(MAX_COLS, col + Math.max(1, n(0, 1)));
    case 'D': return Math.max(0, col - Math.max(1, n(0, 1)));
    case 'K': {
      // Erasing leaves the cursor where it is, however much it erases.
      const how = n(0, 0);
      if (how === 0) cells.length = col;
      else if (how === 1) for (let i = 0; i <= col; i++) cells[i] = { ch: ' ', cls: '' };
      else cells.length = 0;
      return col;
    }
    default: return col;
  }
}

// What one "ESC [ … m" does. Backgrounds are left alone: a log that paints its
// own background fights the page it is read on, and the page wins.
function paint(on: Pen, params: string | undefined): void {
  const codes = (params || '0').split(';').map((x) => Number(x) || 0);
  for (let i = 0; i < codes.length; i++) {
    const n = codes[i];
    if (n === 0) { on.fg = ''; on.bold = on.dim = on.italic = on.under = false; }
    else if (n === 1) on.bold = true;
    else if (n === 2) on.dim = true;
    else if (n === 3) on.italic = true;
    else if (n === 4) on.under = true;
    else if (n === 22) on.bold = on.dim = false;
    else if (n === 23) on.italic = false;
    else if (n === 24) on.under = false;
    else if (n >= 30 && n <= 37) on.fg = FG[n - 30];
    else if (n >= 90 && n <= 97) on.fg = FG[n - 90];
    else if (n === 39) on.fg = '';
    else if (n === 38 || n === 48) {
      // An extended colour carries its own parameters, which are not codes.
      const taken = codes[i + 1] === 5 ? 2 : codes[i + 1] === 2 ? 4 : 1;
      if (n === 38 && codes[i + 1] === 5 && codes[i + 2] < 16) on.fg = FG[codes[i + 2] % 8];
      i += taken;
    }
  }
}

// A line as docker hands it over: an RFC 3339 stamp, a space, the rest. The
// stamp is split off so it can be shown as a clock time, or not at all.
const STAMP = /^(\d{4}-\d{2}-\d{2}T[\d:.]+Z?) (.*)$/s;
// What a line looks like when it is bad news. Only a word on its own, near the
// front, counts: "errors: 0" in the middle of a line is not an error.
const LEVEL: [RegExp, string][] = [
  [/(^|[\s[|"'(])(error|fatal|critical|panic|exception|traceback)([\s\]:|"')]|$)/i, 'err'],
  [/(^|[\s[|"'(])(warn|warning|deprecated)([\s\]:|"')]|$)/i, 'warn'],
  [/(^|[\s[|"'(])(debug|trace)([\s\]:|"')]|$)/i, 'dim'],
];

export interface LogLine { id: number; at: number; text: string; parts: Run[]; level: string }

export function parseLogLine(id: number, line: string): LogLine {
  const m = STAMP.exec(line);
  const { text, parts } = parseAnsi(m ? m[2] : line);
  let level = '';
  const head = text.slice(0, 160);
  for (const [re, name] of LEVEL) if (re.test(head)) { level = name; break; }
  return { id, at: m ? Date.parse(m[1]) : 0, text, parts, level };
}
