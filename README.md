# multiversectl

[![CI](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml/badge.svg)](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Run several complete copies — **universes** — of a multi-repository project on
one machine, so you can work on branches in parallel without port collisions or
stepping on your own checkouts.

**[Documentation](https://multiversectl.dev/)** ·
[Quick start](https://multiversectl.dev/start/quickstart.html) ·
[Manifest reference](https://multiversectl.dev/reference/manifest.html) ·
[Commands](https://multiversectl.dev/reference/cli.html)

## What it solves

A multi-repository project usually has one development stack, and it can only be
in one state at a time. The moment you want a second — a branch to review while
yours keeps running, a migration to try on a throwaway database, an agent that
needs a whole environment — the ports are taken, the containers and volumes have
the same names, and the database holds the other branch's schema.

multiversectl makes the second copy cheap by **namespacing instead of
virtualizing**: nothing is nested, duplicated or emulated.

- One compose project per app per universe, so the runtime itself
  namespaces containers, networks and volumes — including a database per
  universe.
- One git worktree per repository, sharing the clone's object store: no second
  download, and every checkout is a real directory you edit with your own tools.
- Hostname routing through one shared reverse proxy, so no universe publishes a
  host port and there is no port arithmetic to keep in sync. Hostnames can
  resolve from any device on your network, a phone included, or stay on the
  machine.

Bootstrapping another universe is a worktree and a `compose up`: seconds, on the
same daemon and the same image cache you already use.

## Why not a VM, LXC or nested containers

Those give you isolation and charge for it: a guest OS per environment, GBs of
RAM and disk, a provisioning step to keep in sync, another image cache, slower
access to your source, and tooling that has to reach inside the guest. For "run
my own project twice" that is enough machinery to make each new copy expensive
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
binary later. [Install](https://multiversectl.dev/start/install.html) lists what
the machine needs.

## Projects and catalogs

A project someone points you at needs one command, with nothing set up first:

```sh
multiversectl project install acme/multiverse   # owner/repo, any git URL, or a path
multiversectl universe new 1 && multiversectl universe up 1
```

To keep your own index of projects, which can nest other people's, make a
catalog. It is a git repository of pointers, private if you let `init` create it:

```sh
multiversectl init                      # asks where it should live
multiversectl catalog add acme/billing  # asks which node of the tree it goes in
multiversectl project init              # a guide: pick its repositories, create its repository, list it
multiversectl catalog list              # the tree, with what is installed marked
multiversectl project status                    # what is installed here, and what has drifted
multiversectl project update                    # catch up: manifests and new repositories
```

See [Catalogs](https://multiversectl.dev/concepts/catalogs.html).

All of it is also a live page — the tree, every universe and what runs in it —
with buttons for the same operations:

```sh
multiversectl server start --open   # a live page: versions, and start/stop/create/remove, behind a token
multiversectl server stop
```

An agent needs only the command line: `multiversectl universe ls -o json` prints
one compact line with the project's paths and, per universe, its state, folders
and URLs. `--json` works on every command, and `--help --json` describes any of
them as a document.

## Quick start

```sh
multiversectl start acme/multiverse     # install it, make universe 1, run it, print its address
```

Or the steps it takes, one at a time:

```sh
multiversectl project install ./multiverse.yaml --root ~/acme
multiversectl universe new 1            # universe 1: a worktree of every repo on branch u1
multiversectl universe new fix/login    # or name it after the branch it checks out
multiversectl universe up fix/login     # start its containers
multiversectl universe ls               # list universes and their state
multiversectl universe ls -o wide       # every service, whether it is up, and what it owns
multiversectl universe urls fix/login   # print the hostnames it answers on
multiversectl universe doctor           # check prerequisites and common problems
multiversectl universe rm fix/login     # stop and delete it
```

The [quick start](https://multiversectl.dev/start/quickstart.html)
walks through the same thing with a manifest to copy.

## Manifest

One file describes the project: its repositories, how a universe is addressed,
and only the apps that need something other than the defaults. It does not
describe containers — the repositories' compose files already do, and
multiversectl reads them rather than making you repeat them.

```yaml
apiVersion: multiverse/v1alpha1
kind: Project
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

That is a working project: each repository gets an app of its own name, its
compose files are found by convention, and its addresses come from their labels.
[examples/multiverse.yaml](examples/multiverse.yaml) is a commented example with
the rest, and every field is documented in the
[manifest reference](https://multiversectl.dev/reference/manifest.html)
and in the generated [schema](schema/project.v1alpha1.yaml).

## How it works

Each universe is a git worktree of every repository plus one compose project per
app, reached through a shared reverse proxy on per-universe hostnames. The
routing labels, the proxy network and the dropping of published ports are
generated per universe, so your compose files stay the ones you already run.

The documentation goes into
[networking](https://multiversectl.dev/concepts/networking.html),
[storage and data](https://multiversectl.dev/concepts/storage.html),
[overlays and merge order](https://multiversectl.dev/concepts/overlays.html)
and [labels](https://multiversectl.dev/concepts/labels.html).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). The
documentation site is generated: edit [docsrc/](docsrc/) and run `make docs`.

## License

Apache License 2.0, see [LICENSE](LICENSE).

The name and marks are not licensed with the code ([TRADEMARK.md](TRADEMARK.md)),
and contributions are made under the [CLA](CLA.md).
