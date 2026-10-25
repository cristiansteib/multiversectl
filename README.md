# multiversectl

[![CI](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml/badge.svg)](https://github.com/cristiansteib/multiversectl/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Run several complete copies — **universes** — of a multi-repository project on
one machine, so you can work on branches in parallel without port collisions or
stepping on your own checkouts.

**[Documentation](https://cristiansteib.github.io/multiversectl/)** ·
[Quick start](https://cristiansteib.github.io/multiversectl/start/quickstart.html) ·
[Manifest reference](https://cristiansteib.github.io/multiversectl/reference/manifest.html) ·
[Commands](https://cristiansteib.github.io/multiversectl/reference/cli.html)

## What it solves

A multi-repository project usually has exactly one development stack, and that
stack can only be in one state at a time. The moment you want a second one — a
branch to review while yours keeps running, a migration to try on a throwaway
database, an agent that needs a whole environment to itself — you hit the same
wall: the ports are taken, the containers and volumes have the same names, and
the database already has the other branch's schema in it.

multiversectl makes the second copy cheap, by **namespacing instead of
virtualizing**: nothing is nested, duplicated or emulated.

- One container-runtime project per app per universe, so the runtime itself
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

Those give you isolation, and charge for it: a guest OS per environment, GBs of
RAM and disk, a provisioning step that has to stay in sync with your real setup,
another image cache, slower filesystem access to your source, and tooling —
editor, debugger, language server — that now has to reach inside the guest. For
"run my own project twice" that is a lot of machinery to maintain, and it makes
each new copy expensive enough that you stop making them.

The honest trade-off: this is **not** isolation. Universes share your kernel,
your container daemon and your user, so one can saturate the CPU or fill the
disk, and the security boundary is only the one your runtime already gives you.
If you need to run code you do not trust, or a different kernel, or
byte-reproducible machine state, use a VM — that is a different problem. This
tool is for when you trust your own code and want many copies of it, cheaply.

## Install

```sh
# Script (linux/darwin, amd64/arm64). Set MULTIVERSECTL_VERSION to pin a release.
curl -fsSL https://raw.githubusercontent.com/cristiansteib/multiversectl/main/install.sh | sh

# mise
mise use -g ubi:cristiansteib/multiversectl
```

A `.deb` is on the [releases page](https://github.com/cristiansteib/multiversectl/releases),
and `make install` builds from source into `~/.local/bin`. The script and the
`.deb` also provide a short alias, `mvctl`. Later, `multiversectl self-update`
replaces the binary with the latest release.

See [Install](https://cristiansteib.github.io/multiversectl/start/install.html)
for what it needs on the machine.

## Projects and catalogs

A project someone points you at needs one command, with nothing set up first:

```sh
multiversectl install acme/multiverse   # owner/repo, any git URL, or a path
multiversectl universe new 1 && multiversectl universe up 1
```

To keep your own index of projects, which can nest other people's, make a
catalog. It is a git repository of pointers, private if you let `init` create it:

```sh
multiversectl init                      # asks where it should live
multiversectl catalog add acme/billing  # asks which node of the tree it goes in
multiversectl catalog                   # the tree, with what is installed marked
multiversectl status                    # what is installed here, and what has drifted
multiversectl update                    # catch up: manifests and new repositories
```

See [Catalogs](https://cristiansteib.github.io/multiversectl/concepts/catalogs.html).

To see all of it in a page (the tree, what is installed, every universe and what
runs in it), without a way to change anything:

```sh
multiversectl-server --open   # read-only, loopback only
```

## Quick start

```sh
multiversectl install ./multiverse.yaml --root ~/acme
multiversectl universe new 1            # universe 1: a worktree of every repo on branch u1
multiversectl universe new fix/login    # or name it after the branch it checks out
multiversectl universe up fix/login     # start its containers
multiversectl universe ls               # list universes and their state
multiversectl universe ls -o wide       # every service, whether it is up, and what it owns
multiversectl universe urls fix/login   # print the hostnames it answers on
multiversectl doctor           # check prerequisites and common problems
multiversectl universe rm fix/login     # stop and delete it
```

The [quick start](https://cristiansteib.github.io/multiversectl/start/quickstart.html)
walks through the same thing with a manifest to copy.

## Manifest

One file describes the workspace: the repositories a project is made of, how a
universe is addressed, and only the apps that need something other than the
defaults. It does not describe containers — the repositories' own compose files
already do, and multiversectl reads them rather than making you repeat them.

```yaml
apiVersion: multiverse/v1alpha1
kind: Workspace
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

That is a working workspace: each repository gets an app of its own name, its
compose files are found by convention, and its addresses come from their labels.
[examples/multiverse.yaml](examples/multiverse.yaml) is a commented example with
the rest, and every field is documented in the
[manifest reference](https://cristiansteib.github.io/multiversectl/reference/manifest.html)
and in the generated [schema](schema/workspace.v1alpha1.yaml).

## How it works

Each universe is a git worktree of every repository in the manifest plus one
container-runtime project per app. Universes are reached through a shared
reverse proxy on per-universe hostnames, and the routing labels, the proxy
network and the dropping of published ports are generated per universe — your
compose files stay the ones you already run.

The documentation goes into
[networking](https://cristiansteib.github.io/multiversectl/concepts/networking.html),
[storage and data](https://cristiansteib.github.io/multiversectl/concepts/storage.html),
[overlays and merge order](https://cristiansteib.github.io/multiversectl/concepts/overlays.html)
and [labels](https://cristiansteib.github.io/multiversectl/concepts/labels.html).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). The
documentation site is generated: edit [docsrc/](docsrc/) and run `make docs`.

## License

Apache License 2.0, see [LICENSE](LICENSE).

The name and marks are not licensed with the code ([TRADEMARK.md](TRADEMARK.md)),
and contributions are made under the [CLA](CLA.md).
