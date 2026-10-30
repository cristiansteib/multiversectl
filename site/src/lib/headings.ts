// A page's table of contents is read from the page itself.
//
// Page.astro renders its slot to a string and scans that string for <h2 id>,
// exactly as the Go generator scanned the content fragments. The content and
// the contents column therefore cannot drift: a heading appears in the column
// when, and only when, it carries an id, and a heading that should not be a
// landmark simply leaves the id off.
//
// This is a scan and not a parse because the headings are written by hand, in
// one line, in the same shape on every page:
//   <h2 id="binary"><a href="#binary">Ready-made binaries</a></h2>

export interface Heading {
  /** The heading's id, which the contents column links to. */
  id: string;
  /** Its text, as HTML: it comes from our own pages, entities and all. */
  label: string;
}

const H2 = /<h2 id="([^"]+)"[^>]*>(?:<a [^>]*>)?([\s\S]*?)(?:<\/a>)?<\/h2>/g;
const TAG = /<[^>]+>/g;

/** Every <h2 id> of a rendered page, in document order. */
export function headings(html: string): Heading[] {
  const out: Heading[] = [];
  for (const m of html.matchAll(H2)) {
    const label = (m[2] ?? '').replace(TAG, '').trim();
    if (!label) continue;
    out.push({ id: m[1]!, label });
  }
  return out;
}
