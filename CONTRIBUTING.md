# Contributing

Pull requests are welcome.

- Opening a pull request means you agree to the [CLA](CLA.md) for that
  contribution. There is no form to sign; the agreement is the one in that file.
- Sign your commits off (`git commit -s`) so the record of that agreement sits
  in the history.
- Keep the layers honest: `internal/domain` stays pure, `internal/app` depends
  only on the ports in `internal/app/ports.go`, and anything that talks to git,
  docker or the filesystem lives in `internal/adapters`.
- `gofmt`, `go vet ./...` and `go test ./...` must pass; CI runs exactly that.
- `schema/multiverse.v1alpha1.yaml` is generated from the Go types and
  committed, and CI fails when it is stale (`make schema`). The documentation
  site in [`site/`](site/) is built, not committed: `make docs` (needs Node)
  builds it into `site/dist`, and each release builds and publishes it with its
  version. Edit the source, not the output.
- Tests for logic worth testing, not for coverage's sake.
- Conventional commit subjects (`feat:`, `fix:`, `docs:`) — the changelog is
  generated from them.

The project's name and marks are not covered by the licence; see
[TRADEMARK.md](TRADEMARK.md).
