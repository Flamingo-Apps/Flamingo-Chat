# Flamingo Chat

Read this fully before doing anything. Then read the docs it points to - don't start writing code off this file alone, it's a map, not the territory.

## What this is

A campus-wide anonymous chat app (gender-wise matching, group chat), rebuilt from an earlier prototype of the same name that got ~400 users over its first day or two before usage dropped off. This build is also a deliberate hands-on exercise in distributed systems: gRPC, message queues, caching, observability, Kubernetes. Two things are both true and both matter: it needs to actually work for real students, and it's being built to learn and to show real, measured engineering results (portfolio/resume value) - not to fake either one.

**Read the docs before working, every session, not just once:**

- [docs/PRD.md](docs/PRD.md) - what we're building, scope, resolved decisions, open questions
- [docs/SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md) - service boundaries, data flows, gRPC contracts, observability, infra/Kubernetes plan
- [docs/PLAN.md](docs/PLAN.md) - build order, phase by phase, checklist style. **Check this before picking up work** - it says what's next and what's deliberately deferred.
- [docs/CONCEPT.md](docs/CONCEPT.md) - original pitch and the longer-term (phase 2, dating-mechanic) direction, not being built yet
- [docs/kb/](docs/kb/README.md) - running knowledge base, one `.md` file per distributed-systems topic learned along the way (protobuf/buf, Go workspaces, etc.). Add to this as new concepts come up - don't just explain something once and let it evaporate.

If something you're about to build contradicts one of these docs, stop and flag it rather than silently deviating - either the doc is stale and needs updating, or you're about to redo a decision that was already made for a reason.

## Explain as you go - this is a learning project

The person building this is explicitly learning distributed systems through it (gRPC, message queues, caching, observability, Kubernetes). When you implement something non-trivial, briefly explain *why* it's built that way, not just *what* it does - e.g. why a message goes through Redis pub/sub instead of a direct call, why the persistence worker is decoupled from the request path, why a proto field is structured a certain way. A couple of sentences is enough; this isn't a request for a tutorial, it's a request to not treat the person as a passive recipient of finished code.

## Current state (check this is still accurate - update this section if it drifts)

- **Phase 0 is complete and merged to `main`** (PR #1, `phase-0-scaffold` → `main`). CI passed on all 9 module jobs before and after merge. That branch can be deleted locally/remotely whenever - nothing else depends on it.
- Monorepo skeleton: `go.work`, one Go module per service under `services/` (gateway, identity, matching, chat, presence, moderation, persistence-worker), each with a minimal `main.go`. Non-gateway services start a gRPC server with health-check + reflection registered and a `TODO: register the <name> service implementation` - no business logic yet. Gateway starts a plain HTTP server with `/healthz` and a TODO for WebSocket/JWT/gRPC routing.
- Real `.proto` files exist for identity, chat, matching, presence, moderation under `proto/*/v1/`, already `buf generate`-d into `proto/gen/go/`. Treat these as the current source of truth for inter-service contracts, but they're still early - expect fields to change as services get built out.
- `pkg/config` has a tiny env-var config loader (`config.String`, `config.Int`). Use it rather than reading `os.Getenv` directly, for consistency.
- `scripts/scaffold-service.sh <name> <port>` generates the same minimal gRPC skeleton for a brand new service. It refuses to overwrite an existing service directory - don't re-run it against a service that already has real code.
- `deploy/docker-compose.yml` runs Postgres, Redis, RabbitMQ for local dev (`cd deploy && docker compose up -d`). No service reads their connection env vars yet - that wiring happens as each service gets built out. Jaeger and per-service containers are deliberately not in there yet (later phases).
- `.github/workflows/ci.yml` builds + tests every module on push/PR, one matrix job per module.
- All modules (`pkg`, `proto/gen/go`, every `services/*`) pin `google.golang.org/grpc` to `v1.71.1` (not `@latest`) - `@latest` pulled in a version requiring Go 1.25, and the toolchain download for that failed repeatedly on this network. See [docs/kb/01-go-fundamentals/01-03-modules-and-toolchains.md](docs/kb/01-go-fundamentals/01-03-modules-and-toolchains.md) before bumping grpc or other deps. Also note: `go build ./...` from the repo root doesn't expand across nested workspace modules on this Go version - build per-service (`cd services/chat && go build ./...`) instead; see [docs/kb/01-go-fundamentals/01-02-go-workspaces.md](docs/kb/01-go-fundamentals/01-02-go-workspaces.md).
- `docs/kb/` is organized by topic folder (`01-go-fundamentals/`, `02-protobuf-and-grpc/`, `03-databases/`, `04-auth-and-verification/`, `05-infra-and-devops/` so far) - see [docs/kb/README.md](docs/kb/README.md) for the full index.
- Not started yet: any actual service logic, tests, moderation/persistence-worker business logic (deliberately last per PLAN.md). **Next up per PLAN.md: Phase 1** - Identity, Gateway, Chat, Matching, Presence, real-time path only, no durable history yet.

## Working across multiple concurrent sessions

Multiple sessions may be building different services at the same time. To avoid stepping on each other:

- **One branch per service/task** (e.g. `service/chat`, `service/identity`), not everyone on one branch. Commit scoped to your service's directory.
- **Proto changes are the one thing that isn't isolated** - every service depends on `proto/gen/go`. If you need to change a `.proto` file: keep the change minimal and additive where possible, run `buf generate` yourself, call it out clearly in your commit/PR description, and expect other in-flight sessions to need to pull it before their build works. Don't casually redesign a message shape that another session's work depends on without flagging it.
- Check `docs/PLAN.md` for what's already claimed/in-progress vs. genuinely next, so two sessions don't unknowingly start the same service.
- If you finish a checklist item in `docs/PLAN.md`, check it off in that same commit.

## Tech stack quick reference

Go everywhere - gRPC between services (buf-generated stubs) - RabbitMQ for async message delivery/persistence - Redis for presence, caching, and pub/sub fan-out - Postgres as source of truth - Prometheus + Grafana + OpenTelemetry/Jaeger for observability - k3s for deployment. Full reasoning and data flows for all of this are in `docs/SYSTEM_DESIGN.md` - this line is a reminder of the vocabulary, not a substitute for reading it.
