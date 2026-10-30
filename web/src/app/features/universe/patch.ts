// A diff is read line by line, and what each line is comes from the mark it
// starts with. The text of a line is somebody's code, so it is never trusted:
// the mark is split from it for drawing (see patch-view), the code is coloured
// by highlight.js, which escapes what it emits, and Angular sanitises the
// result again on its way into the DOM. Two passes, because one of them is a
// dependency's promise and the other is not.

export type PatchKind = 'hunk' | 'meta' | 'ins' | 'rem' | '';
export interface PatchLine { kind: PatchKind; text: string }

/**
 * Everything before the first hunk is the header; after it a line is what its
 * first character says, whatever it looks like: removing the comment
 * "-- drop table" is the line "--- drop table", and is a removal.
 */
export function classifyPatch(patch: string): PatchLine[] {
  let inHunk = false;
  const kind = (l: string): PatchKind => {
    if (l.startsWith('@@')) { inHunk = true; return 'hunk'; }
    if (!inHunk) return 'meta';
    if (l.startsWith('+')) return 'ins';
    if (l.startsWith('-')) return 'rem';
    if (l.startsWith('\\')) return 'meta';
    return '';
  };
  const lines = patch.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return lines.map((text) => ({ kind: kind(text), text: text || ' ' }));
}

export interface FileLike { path: string; status: string; added: number; removed: number; oldPath?: string; binary?: boolean }
export type TreeRow =
  | { kind: 'dir'; depth: number; name: string; full: string; shut: boolean }
  | { kind: 'file'; depth: number; name: string; file: FileLike };

/**
 * The files as folders, the way GitHub and GitLab list them, flattened for
 * drawing: a folder that holds only another folder is written as one path, and
 * a filter shows what matches wherever it is (folders are then never shut).
 */
export function fileTree(files: FileLike[], filter: string, collapsed: ReadonlySet<string>): TreeRow[] {
  interface Node { dirs: Map<string, Node>; files: { name: string; f: FileLike }[] }
  const root: Node = { dirs: new Map(), files: [] };
  const needle = filter.toLowerCase();
  for (const f of files) {
    if (needle && !f.path.toLowerCase().includes(needle)) continue;
    const parts = f.path.split('/');
    let node = root;
    for (const part of parts.slice(0, -1)) {
      let next = node.dirs.get(part);
      if (!next) { next = { dirs: new Map(), files: [] }; node.dirs.set(part, next); }
      node = next;
    }
    node.files.push({ name: parts[parts.length - 1], f });
  }
  const out: TreeRow[] = [];
  const walk = (node: Node, path: string, depth: number) => {
    for (const [first, child] of [...node.dirs.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
      let name = first;
      let dir = child;
      let full = path ? `${path}/${name}` : name;
      while (dir.dirs.size === 1 && !dir.files.length) {
        const [[n, d]] = [...dir.dirs.entries()];
        name += '/' + n; full += '/' + n; dir = d;
      }
      const shut = !needle && collapsed.has(full);
      out.push({ kind: 'dir', depth, name, full, shut });
      if (!shut) walk(dir, full, depth + 1);
    }
    for (const { name, f } of node.files.sort((a, b) => a.name.localeCompare(b.name))) out.push({ kind: 'file', depth, name, file: f });
  }
  walk(root, '', 0);
  return out;
}

export const STATUS_MARK: Record<string, string> = { added: 'A', modified: 'M', deleted: 'D', renamed: 'R', untracked: 'U' };

/** Only a web address becomes a link: the field is free text that anyone with a shell wrote. */
