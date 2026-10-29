// The site's menu and its page order: the one place a page is declared.
//
// Ported from docsrc/nav.yaml, which the Go generator read. The sidenav, the
// top bar's section links, the breadcrumbs and the previous/next pager are all
// derived from this file, so adding a page here is the only step that puts it
// in the menu, and a page that is not here is unreachable.
//
// A page's `path` is both its address without an extension ("concepts/
// networking") and, by convention, the .astro file under src/pages that
// renders it.

export interface NavPage {
  /** Address without an extension, e.g. "concepts/networking". */
  path: string;
  /** Heads the page and names it in the browser and in the pager. */
  title: string;
  /** Label in the menu, where there is less room. Defaults to `title`. */
  nav?: string;
  /** The lede under the title, and the meta description unless `description` is set. */
  summary: string;
  /** Meta description and og:description, 120-160 characters, when the lede does
   *  not fit that (too long, too short). The on-page lede stays `summary`. */
  description?: string;
  /** The one page that opens with the landing header instead of a title and a
   *  table of contents, and has no sidenav. */
  hero?: boolean;
  /** Drops the "on this page" column and widens the content column, for a page
   *  whose content needs the width more than the reader needs the landmarks. */
  notoc?: boolean;
}

export interface NavGroup {
  title: string;
  pages: NavPage[];
}

export const groups: NavGroup[] = [
  {
    title: "Start here",
    pages: [
      {
        path: "index",
        title: "multiversectl",
        nav: "Home",
        hero: true,
        summary: "Run your whole multi-repository stack as many times as you need, side by side, on one machine. Every copy gets its own branch, its own containers and its own hostnames — so reviews, agents and benchmarks run in parallel instead of taking turns.",
        description: "Run your whole multi-repository stack several times at once on one machine. Every copy gets its own branch, containers and hostnames, so nothing takes turns.",
      },
      {
        path: "docs",
        title: "Documentation",
        nav: "Documentation",
        summary: "How a universe is put together, and where everything else is: the visual pages first, then the reference.",
        description: "Start here: how a multiversectl universe is put together and where everything else is, with the visual guides first and the reference after them.",
      },
      {
        path: "start/machine",
        title: "What's on your machine",
        nav: "On your machine",
        summary: "One picture of it all: what the machine keeps and every universe shares, what one universe owns, and what is rewritten on every start — with networking, data and what is actually running.",
        description: "What multiversectl puts on your machine: what is shared and permanent, what one universe owns, and what is regenerated every time a universe starts.",
      },
      {
        path: "start/lifecycle",
        title: "Life of a universe",
        notoc: true,
        summary: "What exists after new, up, down and rm, what each of them keeps, and the order up does its work in.",
        description: "What exists after multiversectl new, up, down and rm, what each command keeps or deletes, and the order in which up does its work.",
      },
      {
        path: "start/install",
        title: "Install",
        summary: "One binary for Linux and macOS, from a script, mise, a .deb or source.",
        description: "Install multiversectl, one binary for Linux and macOS, from the install script, mise, a .deb package or source, and check that it works.",
      },
      {
        path: "start/quickstart",
        title: "Quick start",
        summary: "Describe a multiverse, create a universe and open it — from nothing to two running copies of a multiverse.",
        description: "Describe a multiverse, create a universe and open it: go from nothing to two running copies of your stack in a few commands with multiversectl.",
      },
    ],
  },
  {
    title: "In depth",
    pages: [
      {
        path: "start/concepts",
        title: "Concepts at a glance",
        nav: "Concepts at a glance",
        summary: "The five words this documentation uses — multiverse, universe, app, host and entrypoint — and how they fit together.",
        description: "The five words this documentation uses (multiverse, universe, app, host and entrypoint) and how they fit together, explained in one short page.",
      },
      {
        path: "start/scope",
        title: "What it does, and what it is not",
        nav: "What it does",
        summary: "Everything it gives you, feature by feature, and the jobs it is the wrong tool for, with what to use instead.",
        description: "Everything multiversectl gives you, feature by feature, and the jobs it is the wrong tool for, with the tools to use for those instead.",
      },
      {
        path: "concepts/universes",
        title: "Universes",
        summary: "What a universe is made of, how it is named, and what it does and does not share with the others.",
        description: "What a universe is made of, how multiversectl names it, and what it shares with the other universes of the same multiverse and what it keeps private.",
      },
      {
        path: "concepts/networking",
        title: "Networking",
        summary: "One shared proxy, one hostname per address per universe, and why no universe publishes a port of its own.",
        description: "One shared proxy, one hostname per address per universe, and why no universe publishes a port of its own, so any number can run at once.",
      },
      {
        path: "concepts/storage",
        title: "Storage and data",
        summary: "Where a universe's code, images, volumes and secrets live, which of them are shared, and what removing one destroys.",
        description: "Where a universe's code, images, volumes and secrets live on disk, which of them are shared between universes, and what removing one destroys.",
      },
      {
        path: "concepts/overlays",
        title: "Overlays and merge order",
        summary: "The files that make up an app, the order they are merged in, and where to put a value that only this environment knows.",
        description: "The files that make up an app, the order multiversectl merges them in, and where to put a value that only one environment knows about.",
      },
      {
        path: "concepts/labels",
        title: "Labels",
        summary: "How a compose file describes itself: the addresses it answers on, the ports it must keep, and the values it needs from its universe.",
      },
      {
        path: "concepts/catalogs",
        title: "Catalogs",
        summary: "An index of multiverses and of other indexes, kept in git, that says what exists and leaves what is installed to each machine.",
      },
      {
        path: "start/compare",
        title: "Compared with other tools",
        nav: "Compared with others",
        summary: "A feature table of multiversectl next to Docktree, container-use, Tilt, Skaffold, Garden and Preevy, and when to pick another.",
      },
    ],
  },
  {
    title: "Tasks",
    pages: [
      {
        path: "tasks/branches",
        title: "Work across branches",
        summary: "Put a feature in its own universe, override one repository's branch, add a repository, and remove a universe when you are done.",
      },
      {
        path: "tasks/reach",
        title: "Reach a universe",
        summary: "From another device, from a domain you own, from a shell, and from a database client.",
        description: "Reach a running universe from another device, from a domain you own, from a shell inside a container, and from a database client on your laptop.",
      },
      {
        path: "tasks/services",
        title: "Control which services run",
        summary: "Start optional services on demand, keep a published port, and give a service its own hostname.",
        description: "Start optional services on demand, keep a published port when you need one, and give a service its own hostname, one task at a time.",
      },
      {
        path: "tasks/catalog",
        title: "Manage many multiverses from one index",
        summary: "Set up a root catalog, nest another one under it, list multiverses in it, and install only the ones this machine needs.",
        description: "Set up a root catalog, nest another catalog under it, list multiverses in it, and install only the multiverses that this machine needs.",
      },
      {
        path: "tasks/server",
        title: "Explore and run it in a browser",
        summary: "A live web view of the catalog, the multiverses and the fleet of universes, with versions and buttons to start, stop, create and remove universes, behind a token.",
        description: "The web view: a live page of your catalog, multiverses and every universe, with buttons to start, stop, create and remove them, behind a token.",
      },
    ],
  },
  {
    title: "Reference",
    pages: [
      {
        path: "reference/cli",
        title: "Commands",
        summary: "Every command, what it does and the arguments it takes.",
        description: "Every multiversectl command in one table: what it does, the arguments and flags it takes, and an example of each, from `start` to `system reset`.",
      },
      {
        path: "reference/manifest",
        title: "Manifest",
        summary: "Every field of a multiverse manifest, with its default.",
        description: "Every field of a multiverse manifest and its default: the repositories, apps, entrypoints, hosts and overlays a multiverse.yaml can declare.",
      },
      {
        path: "reference/variables",
        title: "Variables and labels",
        summary: "What a universe passes to the runtime, and the labels a compose file can use to ask for it.",
        description: "What a universe passes to the runtime as environment variables, and the labels a compose file can use to ask multiversectl for each of them.",
      },
      {
        path: "reference/paths",
        title: "Paths and state",
        summary: "What multiversectl writes, where, and which files it generates.",
        description: "Where multiversectl writes: the multiverse root, each universe's worktrees, the files it generates beside your checkouts, and its own state.",
      },
    ],
  },
  {
    title: "Troubleshooting",
    pages: [
      {
        path: "reference/troubleshooting",
        title: "Troubleshooting",
        nav: "Common problems",
        summary: "Hostnames that do not resolve, ports in use, missing secrets and dev servers that answer 403.",
        description: "Fixes for the problems you will hit: hostnames that do not resolve, ports already in use, missing secrets and dev servers that answer 403.",
      },
    ],
  },
];

/** Every page, in reading order: the order of the menu, and of the pager. */
export const pages: NavPage[] = groups.flatMap((g) => g.pages);

const byPath = new Map(pages.map((p) => [p.path, p]));
const sectionByPath = new Map(
  groups.flatMap((g) => g.pages.map((p) => [p.path, g.title] as const)),
);

/** The URL of a page. Paths keep the .html the published site already uses. */
export function href(path: string): string {
  return `/${path}.html`;
}

/** The label a page carries in the menu. */
export function navLabel(p: NavPage): string {
  return p.nav ?? p.title;
}

export function page(path: string): NavPage {
  const p = byPath.get(path);
  if (!p) throw new Error(`page "${path}" is not in src/data/nav.ts`);
  return p;
}

/** The group a page belongs to, which is also its breadcrumb. */
export function section(path: string): string {
  const s = sectionByPath.get(path);
  if (!s) throw new Error(`page "${path}" is not in src/data/nav.ts`);
  return s;
}

export interface PagerLink {
  label: string;
  href: string;
}

/** The pages either side of this one in reading order. */
export function pager(path: string): { prev?: PagerLink; next?: PagerLink } {
  const i = pages.findIndex((p) => p.path === path);
  if (i < 0) throw new Error(`page "${path}" is not in src/data/nav.ts`);
  const link = (p?: NavPage) => (p ? { label: p.title, href: href(p.path) } : undefined);
  return { prev: link(pages[i - 1]), next: link(pages[i + 1]) };
}
