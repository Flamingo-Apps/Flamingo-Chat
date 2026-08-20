# Go modules, MVS, and toolchains

## `go.mod`'s `go` directive vs. the installed toolchain

A `go.mod` file's `go 1.23.0` line is a *minimum language/toolchain version requirement*, not just documentation. Since Go 1.21, the `go` command takes this literally: if any module in the build requires a higher Go version than the one installed, `go` will try to **download that exact toolchain** automatically (`GOTOOLCHAIN=auto`, the default) rather than just erroring.

This bit us directly: running `go get google.golang.org/grpc@latest` pulled in `grpc v1.83.1`, whose `go.mod` requires `go >= 1.25.0`. The installed toolchain was 1.24.3, so `go` tried to silently download a `go1.25.0` toolchain from `proxy.golang.org` - which then failed repeatedly on this network (timeouts/connection resets on the large toolchain zip, even though smaller module downloads from the same proxy worked fine).

Fix: don't fight the network, avoid needing the newer toolchain at all. Pinned `grpc` to `v1.71.1` (requires only `go >= 1.22.0`) instead of `@latest`. Newer isn't automatically better when the network can't reliably fetch what "newer" requires.

## Minimum Version Selection (MVS)

Go's dependency resolution (MVS) picks the *minimum* version that satisfies every requirement in the module graph - not the latest available. So pinning `grpc@v1.71.1` should have been enough to pull its transitive deps (`golang.org/x/net`, etc.) at compatible-but-old versions too.

In practice it wasn't that clean: `go.mod` already had leftover `// indirect` requirement lines from an earlier `go get @latest` (e.g. `golang.org/x/net v0.55.0`), and those leftover lines act as their own floor - MVS won't silently lower an explicit requirement just because nothing needs it that high anymore. `go mod tidy` will *add or remove* requirements to match what's actually imported, but it doesn't aggressively downgrade an existing requirement line on its own. Had to explicitly `go get golang.org/x/net@v0.34.0` (etc., matching what `grpc v1.71.1`'s own `go.mod` declared) to actually pull the versions back down.

## `GOTOOLCHAIN=local` as a debugging tool

Setting `GOTOOLCHAIN=local` in the environment forces `go` to use whatever toolchain is actually installed and **error immediately** instead of silently trying to download a newer one. Very useful for diagnosing exactly which module/dependency is demanding a newer Go version, instead of getting a generic network timeout with no clue what triggered it.

## Practical takeaway for this repo

Keep the `go` directive in each `go.mod` (and in `go.work`) at whatever's actually installed locally unless there's a real reason to require newer, and re-check pinned versions occasionally rather than reflexively running `@latest`.
