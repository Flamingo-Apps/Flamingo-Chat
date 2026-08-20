# Go workspaces (`go.work`)

## The problem

Flamingo Chat is a monorepo of independently deployable services (`services/identity`, `services/chat`, etc.), each its own Go module with its own `go.mod`. Independent modules matter because each service builds/deploys separately - one service's dependency bump shouldn't force a rebuild of every other service.

But during development, `services/identity` needs to import shared code from `pkg/config` and generated code from `proto/gen/go`. Normally, importing another module means either:
- publishing it somewhere `go get` can fetch it from, or
- adding a `replace` directive in every consuming `go.mod` pointing at a local path

Both are annoying for a monorepo under active development - you'd be publishing or hand-editing replace paths constantly.

## The fix: workspaces

`go.work` (a feature since Go 1.18) sits at the repo root and lists every module directory that should be resolved locally instead of fetched:

```
go 1.23.0

use (
    ./pkg
    ./proto/gen/go
    ./services/chat
    ./services/gateway
    ...
)
```

With this in place, running `go build`/`go test`/etc. from anywhere inside the workspace resolves imports between these modules straight from disk - no publishing, no `replace` lines in individual `go.mod` files. Each service's `go.mod` stays clean and would still work correctly if built in isolation outside the workspace (e.g. in a Docker build context that only copies that one service's directory plus `pkg`/`proto`).

`go.work` itself is **not** committed dependency information for any one module - it's a local/repo-wide development convenience. It's still checked into git here because everyone working on this repo wants the same workspace wiring.

## Quirk hit in practice

`go build ./...` from the repo root fails on this Go version when the wildcard spans a directory that isn't itself a module root - e.g. `./services/...` errors with "directory prefix services does not contain modules listed in go.work", even though `services/chat` etc. clearly are workspace modules one level deeper. Exact module-rooted patterns work fine: `go build ./services/chat/...`, or just `cd services/chat && go build ./...`. This matches how each service actually gets built for real (its own Docker build stage builds just that module), so it's a non-issue in practice - just surprising the first time.
