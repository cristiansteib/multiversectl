# AGENTS.md

Go CLI that runs several numbered copies (*universes*) of a multi-repository
project side by side: one git worktree per repo, one compose project per app,
hostnames behind one shared Traefik.

## Commands

```sh
make build        # ./dist/multiversectl
make test lint    # go test ./... ; gofmt -l + go vet
make schema       # regenerate schema/workspace.v1alpha1.yaml from the Go types
make schema-check # fail if that file is stale (CI runs it)
make docs         # regenerate docs/ from docsrc/ (sources, layout and menu)
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
| `internal/tools/docgen` | Renders `docs/` from `docsrc/`; see `docsrc/README.md`. |

A new capability starts as a port method in `internal/app/ports.go`.

## Conventions

- Sign your commits off (`git commit -s`); see [CONTRIBUTING.md](CONTRIBUTING.md).
- Conventional commits, and **`fix:` while the project is pre-1.0** —
  `feat:` cuts a minor release, and releases should stay on patches for now.
- Comments say *why*, not what. Don't narrate code that reads plainly.
- Tests cover behaviour that would otherwise be argued about; don't pad.
- Nothing about any private project belongs in this repo — examples use
  `acme`, `example.test` and the like.
