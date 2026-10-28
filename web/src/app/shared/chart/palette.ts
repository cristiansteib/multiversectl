const SLOTS = 8;

/** How a series is told apart: a colour and a dash, so colour is never the only signal. */
export interface SeriesStyle { slot: number; colour: string; dash: string }

const DASHES = ['', '7 4', '2 3', '9 3 2 3', '4 2', '1 4', '12 4', '6 2 1 2'];
const OTHER: SeriesStyle = { slot: 0, colour: 'var(--faint)', dash: '3 3' };

/**
 * A palette keeps one colour per key for as long as that key is around, so the
 * line a reader has learned does not become another thing's line. Past eight
 * keys the rest share the grey of "other" rather than a ninth made-up colour.
 */
export class Palette {
  private readonly held = new Map<string, number>();

  /** Give the keys their colours in the order offered; keys no longer offered let theirs go. */
  claim(keys: readonly string[]): void {
    for (const k of [...this.held.keys()]) if (!keys.includes(k)) this.held.delete(k);
    const taken = new Set(this.held.values());
    for (const k of keys) {
      if (this.held.has(k)) continue;
      let free = 0;
      while (free < SLOTS && taken.has(free)) free++;
      if (free >= SLOTS) break;
      this.held.set(k, free);
      taken.add(free);
    }
  }

  has(key: string): boolean { return this.held.has(key); }

  style(key: string): SeriesStyle {
    const i = this.held.get(key);
    return i === undefined ? OTHER : { slot: i + 1, colour: `var(--s${i + 1})`, dash: DASHES[i] };
  }
}

export const otherStyle = OTHER;
