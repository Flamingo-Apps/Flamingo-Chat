# AGENTS.md

Rules and context for AI coding agents working in this repository. Human contributors should start with [CONTRIBUTING.md](CONTRIBUTING.md); the rules here apply to them too.

## The project

Flamingo Chat is an anonymous chat app for one campus (KIIT): group rooms, random 1:1 matching, and gender-filtered matching for verified students. It rebuilds an earlier prototype that reached about 400 users and lost them within two days.

It has two goals, and both matter:

- It has to work for real students.
- It is a hands-on way for the maintainer to learn distributed systems (gRPC, Kafka, Redis, Kubernetes, observability), with results that are measured, not estimated.

## Read before working

- [docs/PRD.md](docs/PRD.md): what is being built and for whom.
- [docs/PLAN.md](docs/PLAN.md): what is done and what is next.
- [docs/decisions.jsonl](docs/decisions.jsonl): why things are the way they are.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): how the system is built and why. Sections marked Proposed are drafted, not yet reviewed with the maintainer - check before treating one as settled.

If what you are about to build contradicts one of these, stop and raise it. Either the document is stale or you are about to undo a decision that was made for a reason.

## Rules

1. **Verify external facts before stating them.** Never state pricing, limits, free tiers, features or behaviour of an outside product or service from memory. Check its current official documentation first and cite the source. If you cannot verify something, say it is unverified. Facts about this repository are verified by reading the code or running a command, not by trusting a document. (DEC-0017)
2. **Discuss significant changes before building.** For anything that changes scope, architecture, infrastructure or a contract between services, present the options with a recommendation and wait for the maintainer's decision. (DEC-0016)
3. **Log decisions and incidents.** When a choice between real alternatives is made, or something breaks, misleads or costs time, append an entry to `docs/decisions.jsonl` in the same pull request. The format is in [CONTRIBUTING.md](CONTRIBUTING.md#decision-log).
4. **Never commit to `main`.** Every change goes through a branch and a pull request, and CI must pass before merge. The workflow is in [CONTRIBUTING.md](CONTRIBUTING.md#workflow). (DEC-0022)
5. **No number without evidence.** Do not write a latency, throughput, user count or cost figure into any document unless a dated artefact with its method is committed to the repository. (DEC-0025)
6. **Keep the documentation small.** Do not add new documentation files without agreeing it first. Reasoning belongs in the decision log; design belongs in the architecture document. (DEC-0018)
7. **Explain as you go.** When you implement something non-trivial, say in a few sentences why it is built that way, not only what it does. The maintainer is learning through this project.
8. **Treat `.proto` changes as shared.** Every service depends on `proto/gen/go`. Keep changes minimal and additive, regenerate with `buf generate`, commit the generated code with the `.proto` change, and call it out in the pull request.
9. **No secrets in the repository.** No credentials, tokens, keys or real user data, in code, tests, fixtures, logs or documents. The repository is public.
10. **No AI attribution.** Do not add a `Co-Authored-By` trailer or any other AI credit to a commit message, and do not add a "generated with" line to a pull request description. `.claude/settings.json` turns both off for Claude Code; configure other tools the same way. (DEC-0034)

## Repository layout

```
services/            one Go module per service, each with its own Dockerfile
  gateway/           public HTTP and WebSocket entry point
  identity/          accounts, pseudonyms, verification
  matching/  chat/  presence/  moderation/  persistence-worker/
proto/               .proto contracts, one directory per service
  gen/go/            generated Go code; never edit by hand
pkg/                 shared Go packages (config, grpclog)
deploy/
  helm/service/      one Helm chart for every Go service; values/<name>.yaml per service
  k8s/dev/           plain manifests for local Postgres, Redis and Kafka
Tiltfile             local development: builds images, deploys everything above
tools/admin/         command-line tool calling Moderation's admin actions (DEC-0029); not deployed, no Dockerfile
scripts/             scaffold-service.sh for a new gRPC service, check-decisions.sh
docs/                PRD, architecture, plan, decision log
web/                 web frontend (planned, not created yet)
```

## Build and test

Each service is its own Go module, tied together by `go.work`. Build and test from inside a module directory; `go build ./...` from the repository root does not span the modules (INC-0002).

```sh
cd services/identity
GOTOOLCHAIN=local go build ./...
GOTOOLCHAIN=local go test ./...
```

- CI builds and tests every module with Go 1.27.3 and `GOTOOLCHAIN=local`, and the service images build from `golang:1.27.3-bookworm`. Every `go.mod` and `go.work` declares `go 1.27.2`, the minimum version, so a patch release that only fixes a standard-library vulnerability moves CI and the Dockerfiles without forcing every local toolchain to move too (INC-0019). Do not add a dependency that needs a newer Go version (INC-0001, DEC-0051), and keep the pin current rather than letting it drift unreviewed (INC-0004).
- In workspace mode Go resolves one version of each dependency for all modules; every module currently requires the same grpc version (v1.84.0). Check with `go list -m google.golang.org/grpc` if that ever looks wrong.
- Local dependencies (Postgres, Redis, Kafka) and every service run together under Tilt, on a local kind cluster: `tilt up`, or `tilt ci` for a one-shot run with no UI. See the README's "Run it locally" section and `docs/ARCHITECTURE.md` §9 (DEC-0039). `deploy/docker-compose.yml` is gone (DEC-0020, DEC-0039) - don't re-add it.

## Code conventions

- Read configuration through `pkg/config` (`config.String`, `config.Int`), not `os.Getenv`.
- Register `pkg/grpclog`'s interceptor in every service that has real RPCs. It logs one structured line per unary request.
- Reach storage through a repository interface with the real implementation behind it, as `services/identity/internal/store` does. Business logic never imports a database driver.
- Return gRPC status codes from handlers (`status.Error(codes.NotFound, ...)`). Stores return their own sentinel errors, and the handler translates them.
- Test business logic against in-memory fakes of those interfaces. Fakes do not reproduce real database behaviour (INC-0003), so storage code also needs tests against the real database.
- Match the surrounding code: its naming, comment density and structure.
