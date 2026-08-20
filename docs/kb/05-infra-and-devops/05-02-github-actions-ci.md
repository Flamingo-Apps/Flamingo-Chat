# GitHub Actions (CI)

## The shape of a workflow

A workflow is a YAML file under `.github/workflows/`. GitHub runs it automatically when its `on:` triggers fire (here: every `push`, and every `pull_request` targeting `main`). A workflow has one or more `jobs`, each job runs on a fresh throwaway VM (`runs-on: ubuntu-latest`) and executes a list of `steps` in order. A step either runs a shell command (`run:`) or invokes a published, reusable "action" (`uses:`) - e.g. `actions/checkout@v4` clones the repo onto the VM, `actions/setup-go@v5` downloads and installs a specific Go version. Actions are just other people's (or GitHub's own) packaged workflow steps, versioned like any dependency.

## Why a matrix, and why per-module

`strategy: matrix:` runs the same job once per value in the list, in parallel VMs, each with `${{ matrix.module }}` substituted. This repo has one `module` entry per Go module (`pkg`, `proto/gen/go`, every `services/*`) rather than one `go build ./...` at the repo root, for two reasons:

1. `go build ./...` from the workspace root doesn't reliably expand across nested workspace modules on this Go version (see [../01-go-fundamentals/01-02-go-workspaces.md](../01-go-fundamentals/01-02-go-workspaces.md)) - `working-directory: <module>` sidesteps that entirely.
2. It mirrors how each service will actually be built later (its own Docker build stage, its own deploy artifact) - a broken module fails independently and CI tells you exactly which one, rather than one big undifferentiated build step.

## `GOTOOLCHAIN: local`

Set as a workflow-level `env:`. Forces the `go` command to use exactly the version `setup-go` installed rather than auto-downloading a different one mid-run if some dependency's `go.mod` asks for a higher version. This is a direct lesson from the grpc/toolchain-download failure documented in [../01-go-fundamentals/01-03-modules-and-toolchains.md](../01-go-fundamentals/01-03-modules-and-toolchains.md) - better to fail fast with a clear "needs a newer Go" error than have CI silently try (and possibly fail) to fetch a toolchain from the network.

## What's deliberately minimal here

No lint step (`buf lint`, `go vet` beyond what `go build` catches), no `buf breaking` check against the previous proto version, no deploy step. This is Phase 0's "basic CI" - just prove build+test pass on every push. Those are reasonable later additions (buf lint in particular, once more than one session is touching `.proto` files - see the open question in [../02-protobuf-and-grpc/02-01-protobuf-and-buf.md](../02-protobuf-and-grpc/02-01-protobuf-and-buf.md)), not missing pieces of this pass.
