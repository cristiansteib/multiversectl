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
- Two things are generated and committed, and a test fails when either is
  stale: `schema/workspace.v1alpha1.yaml` from the Go types (`make schema`) and
  `docs/` from [`docsrc/`](docsrc/) (`make docs`). Edit the source, not the
  output.
- Tests for logic worth testing, not for coverage's sake.
- Conventional commit subjects (`feat:`, `fix:`, `docs:`) — the changelog is
  generated from them.

The project's name and marks are not covered by the licence; see
[TRADEMARK.md](TRADEMARK.md).
