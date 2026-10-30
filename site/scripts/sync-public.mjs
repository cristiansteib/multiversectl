// Copies the one file the site serves but does not own into public/.
//
// install.sh is the project's real installer, committed at the repository
// root, and the home page's one-liner points at https://multiversectl.dev/
// install.sh. It is copied rather than duplicated, so there is one of it; the
// copy is gitignored.
//
// Nothing here reads from docs/. docs/ is this project's build output now, so
// sourcing a file out of it would make the build depend on its own last run.
import { cp, mkdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const site = join(here, '..');
const repo = join(site, '..');

const jobs = [
  { from: join(repo, 'install.sh'), to: join(site, 'public', 'install.sh') },
];

for (const { from, to } of jobs) {
  await mkdir(dirname(to), { recursive: true });
  await cp(from, to, { recursive: true });
  console.log(`synced ${from} -> ${to}`);
}
