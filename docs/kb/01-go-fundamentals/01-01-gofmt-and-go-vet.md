# gofmt and go vet

## gofmt

Go ships its own code formatter, and unlike Prettier/ESLint in JS, there's no configuration file, no style debate - `gofmt`'s output is just what Go code looks like. Every Go codebase you'll ever read is formatted this way, because everyone runs the same tool.

Two commands matter:

```sh
gofmt -l <path>   # list files that aren't formatted correctly (no changes)
gofmt -w <path>   # rewrite them in place
```

Used both while building Identity: `gofmt -l services/identity` flagged `server_test.go` after some hand-aligned struct literals ended up with inconsistent spacing, `gofmt -w` fixed it immediately. Editors with Go support (VS Code's Go extension, GoLand) normally run this automatically on save, so you'd rarely type it by hand day to day - worth confirming that's wired up.

## go vet

`go vet` is a separate static analysis pass that catches suspicious code `go build` doesn't - things like a `Printf` call with the wrong number of format arguments, or a struct passed by value where a lock inside it would get copied. It doesn't check style (that's `gofmt`'s job), only correctness smells.

```sh
go vet ./...
```

Run from inside a module directory (same `./...` wildcard caveat as `go build` - see [01-02-go-workspaces.md](01-02-go-workspaces.md)). Worth running alongside `go build`/`go test` before considering a change done; it's cheap and catches a real class of bugs `go build` is silent about.
