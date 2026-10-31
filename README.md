# multiversectl

[![CI](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml/badge.svg)](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**A full, working copy of a system that spans several repositories — per branch,
per review, per agent — on one machine, from the compose files you already
have.** Each copy, a *universe*, gets a worktree of every repository, its own
containers and data, and hostnames that open from any device on your network, a
phone included.

**[Documentation](https://multiversectl.dev/)** ·
[Quick start](https://multiversectl.dev/docs/start/quickstart/) ·
[Compared with other tools](https://multiversectl.dev/docs/start/compare/) ·
[Manifest reference](https://multiversectl.dev/docs/reference/manifest/) ·
[Commands](https://multiversectl.dev/docs/reference/cli/)

```sh
multiversectl universe new fix/login   # every repository on branch fix/login
multiversectl universe up fix/login    # every app, its own containers and data
multiversectl universe urls fix/login  # http://api-fix-login.192-168-1-20.traefik.me ...
```

## What it solves

A system spread over several repositories usually has one development stack, and
it can only be in one state at a time. The moment you want a second — a branch to
review while yours keeps running, a migration to try on a throwaway database, an
agent that needs a whole environment — the ports are taken, the containers and
volumes have the same names, and the database holds the other branch's schema.

- **The whole system, as one.** A universe is a worktree of every repository on a
  coordinated branch, with every app running. It is created, started, listed and
  removed as one thing, not as stacks you keep in step by hand.
- **Addresses that work.** One shared proxy routes by hostname, so no copy
  publishes a port. Each universe hands its own URLs and origins to the services
  that check them (`API_URL`, `MULTIVERSE_ORIGINS`, or a label), so logins, OAuth
  callbacks and CORS work in every copy. HTTPS when you want it.
- **Shared in one command.** Point someone at a multiverse and
  `multiversectl start acme/platform` clones every repository it names, makes a
  universe and runs it, with nothing set up first. A git URL or a path works
  too, and a repository with only a compose file runs as itself.
- **Nothing to rewrite.** It reads your compose files and describes no
  containers. A repository with one exposed port needs no configuration; labels
  say the rest and are inert when multiversectl is not driving.
- **Cheap copies.** Namespacing, not virtualizing: a worktree sharing the clone's
  objects and a `compose up` on the daemon and image cache you already use. Seconds.
- **A life, not only a start.** Hooks seed data and register addresses, then take
  them back on removal; config files are rendered from the universe's values;
  `doctor` asks every address whether it answers.
- **For agents and people alike.** `--json` on every command and a skill tell an
  agent how to work in a universe of its own; a web view shows every universe,
  what its branches add and their pull requests.

## What it is not

Not a sandbox (universes share your kernel and daemon), not a replacement for
compose, not a rebuild-on-save loop, not a deployment tool, and not a preview
service on the internet. Each universe runs its own services, so the machine's
memory is the limit on how many. The
[comparison](https://multiversectl.dev/docs/start/compare/) says when
[Docktree](https://docktree.dev/),
[container-use](https://github.com/dagger/container-use),
[Tilt](https://tilt.dev/), [Skaffold](https://skaffold.dev/),
[Garden](https://garden.io/) or a preview environment such as
[Preevy](https://preevy.dev/) fits better.

## Why not a VM, LXC or nested containers

Those give you isolation and charge for it: a guest OS per environment, GBs of
RAM and disk, a provisioning step to keep in sync, another image cache, slower
access to your source, and tooling that has to reach inside the guest. For "run
my own multiverse twice" that is enough machinery to make each new copy expensive
enough that you stop making them.

The trade-off, plainly: this is **not** isolation. Universes share your kernel,
daemon and user, so one can saturate the CPU or fill the disk, and the security
boundary is only your runtime's. To run code you do not trust, or a different
kernel, use a VM. This is for when you trust your own code and want many cheap
copies of it.

## Install

```sh
# Script (linux/darwin, amd64/arm64). Set MULTIVERSECTL_VERSION to pin a release.
curl -fsSL https://multiversectl.dev/install.sh | sh

# Homebrew
brew install cristiansteib/tap/multiversectl

# mise
mise use -g ubi:cristiansteib/multiversectl
```

A `.deb` is on the [releases page](https://github.com/cristiansteib/multiversectl/releases),
and `make install` builds from source into `~/.local/bin`. The script and the
`.deb` also provide the alias `mvctl`; `multiversectl self-update` replaces the
binary later. [Install](https://multiversectl.dev/docs/start/install/) lists what
the machine needs.

## Multiverses and catalogs

A multiverse someone points you at needs one command, with nothing set up first:

```sh
multiversectl start acme/multiverse   # owner/repo, any git URL, or a path
multiversectl universe new 1 && multiversectl universe up 1
```

To keep your own index of multiverses, which can nest other people's, make a
catalog. It is a git repository of pointers, private if you let `init` create it:

```sh
multiversectl init                      # asks where it should live
multiversectl catalog add acme/billing  # asks which node of the tree it goes in
multiversectl catalog new               # asks multiverse or catalog, then a guide to make and list it
multiversectl catalog ls                # the tree, with what is installed marked
multiversectl status                    # what is installed here, and what has drifted
multiversectl catalog sync              # catch up: manifests and new repositories
```

See [Catalogs](https://multiversectl.dev/docs/concepts/catalogs/).

All of it is also a live page — the tree, every universe and what runs in it —
with buttons for the same operations:

```sh
multiversectl server up --open      # a live page: versions, and start/stop/create/remove, behind a token
multiversectl server down
```

An agent needs only the command line: `multiversectl universe ls -o json` prints
one compact line with the multiverse's paths and, per universe, its state, folders
and URLs. `--json` works on every command, and `--help --json` describes any of
them as a document.

## Quick start

```sh
multiversectl start acme/multiverse     # install it, make universe 1, run it, print its address
```

Or the steps it takes once the multiverse is installed, one at a time:

```sh
multiversectl universe new 1            # universe 1: a worktree of every repo on branch u1
multiversectl universe new fix/login    # or name it after the branch it checks out
multiversectl universe up fix/login     # start its containers
multiversectl universe ls               # list universes and their state
multiversectl universe ls -o wide       # every service, whether it is up, and what it owns
multiversectl universe urls fix/login   # print the hostnames it answers on
multiversectl doctor                    # check the machine and the multiverse; doctor 1 checks universe 1 too
multiversectl universe rm fix/login     # stop and delete it
```

The [quick start](https://multiversectl.dev/docs/start/quickstart/)
walks through the same thing with a manifest to copy.

## Manifest

One file describes the multiverse: its repositories, how a universe is addressed,
and only the apps that need something other than the defaults. It does not
describe containers — the repositories' compose files already do, and
multiversectl reads them rather than making you repeat them.

```yaml
apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  expose: lan            # or local: this machine only
  domain: auto           # auto follows expose
  repos:
    - name: api
      url: git@github.com:acme/api.git
    - name: web
      url: git@github.com:acme/web.git
```

That is a working multiverse: each repository gets an app of its own name, its
compose files are found by convention, and its addresses come from their labels.
[examples/multiverse.yaml](examples/multiverse.yaml) is a commented example with
the rest, and every field is documented in the
[manifest reference](https://multiversectl.dev/docs/reference/manifest/)
and in the generated [schema](schema/multiverse.v1alpha1.yaml), which the binary
carries: `multiversectl schema spec.apps` prints one field and what it takes,
without the repository or a network.

## How it works

Each universe is a git worktree of every repository plus one compose project per
app, reached through a shared reverse proxy on per-universe hostnames. The
routing labels and the dropping of published ports are generated per universe,
so your compose files stay the ones you already run. The proxy joins each app's
own network, so no universe sees another's services by name.

The documentation goes into
[networking](https://multiversectl.dev/docs/concepts/networking/),
[storage and data](https://multiversectl.dev/docs/concepts/storage/),
[overlays and merge order](https://multiversectl.dev/docs/concepts/overlays/)
and [labels](https://multiversectl.dev/docs/concepts/labels/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). The
documentation site is an Astro project: edit [site/](site/) and run `make docs`,
which builds it into `docs/`, the folder GitHub Pages publishes.

## License

Apache License 2.0, see [LICENSE](LICENSE).

The name and marks are not licensed with the code ([TRADEMARK.md](TRADEMARK.md)),
and contributions are made under the [CLA](CLA.md).
