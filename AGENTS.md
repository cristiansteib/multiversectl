# AGENTS.md

Go CLI that runs several numbered copies (*universes*) of a multi-repository
project side by side: one git worktree per repo, one compose project per app,
hostnames behind one shared Traefik.

## Commands

```sh
make build        # ./dist/multiversectl
make test lint    # go test ./... ; gofmt -l + go vet
make schema       # regenerate schema/multiverse.v1alpha1.yaml from the Go types
make schema-check # fail if that file is stale (CI runs it)
make docs         # build the Astro site (site/) into site/dist; a release builds and publishes it
make ui           # build the Angular app (web/) into the server's embed dir; never committed
make ui-dev       # ng serve with hot reload on :4200, proxied to a running multiversectl-server
```

Run `make schema` after touching `internal/adapters/manifest/v1alpha1/types.go`
or its `+markers`, and commit the result.

## Layout

| Path | Rule |
|---|---|
| `internal/domain` | Pure: naming, hostnames, state. No I/O, no imports from elsewhere in the tree. |
| `internal/app` | Use cases over the ports in `ports.go`. Never reach for docker, git or the filesystem directly. |
| `internal/adapters/*` | One adapter per port. Only these may touch the outside world. |
| `internal/adapters/cli` | Flags and rendering. Colour and tables live here, not in `app`. |
| `internal/tools/schemagen` | `go/ast` generator for the CRD-style schema. |
| `schema/` | The generated field reference, and the package that embeds and reads it: the CRD shape is known here, not by its callers. Generated: `make schema`. |
| `site/` | The documentation site (Astro). `make docs` builds it into `site/dist`; the release workflow builds it again, stamped with the release, and publishes it to Pages. See `site/README.md`. |

A new capability starts as a port method in `internal/app/ports.go`.

## Conventions

- Sign your commits off (`git commit -s`); see [CONTRIBUTING.md](CONTRIBUTING.md).
- Conventional commits, and **`fix:` while the project is pre-1.0** —
  `feat:` cuts a minor release, and releases should stay on patches for now.
- There are two kinds of file and two words for them: a **Multiverse**
  (`kind: Multiverse`, a `multiverse.yaml`: which repositories are needed and how
  to run universes of them) and a **Catalog** (`kind: Catalog`: an index of
  multiverses and other catalogs). A **universe** is the running copy; it has no
  kind. "Project" is not a word of the tool: do not use it, and do not use it for a
  compose project either — that is the **compose project**. Nothing from before the
  rename is read: no old kinds, keys, flags or aliases.
- Comments say *why*, not what. Don't narrate code that reads plainly.
- Tests cover behaviour that would otherwise be argued about; don't pad.
- Nothing about any private project belongs in this repo — examples use
  `acme`, `example.test` and the like. That covers the screenshots too.
- The docs carry screenshots of the web view (`site/public/assets/img`, served
  as `/assets/img`). Change how it looks and they go stale, so retake them:
  `MVCTL_DEV_UI=1 go test ./internal/adapters/server -run TestDevUI -timeout 0`
  serves the invented fleet they are taken from, in light and dark.
