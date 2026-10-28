# web

The web view: an Angular app that `multiversectl-server` serves at `/`. Its build is not committed; the release builds it in its own job and embeds it in the binary.

```sh
make ui       # build it and copy it into internal/adapters/server/webapp (then `go build`)
make ui-dev   # ng serve with hot reload, proxying /api and /ws to a running multiversectl-server
```

`ui-dev` expects the server on 127.0.0.1:7777; set `MULTIVERSECTL_SERVER` for another address. Sign in once with the link the server prints: cookies are shared between ports on localhost.

Layout: `core/` (API, live store, operations), `shared/` (UI pieces, charts, dialogs), `layout/` (the frame), `features/` (one folder per section).
