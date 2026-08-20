# Docker Compose for local dev

## Why this exists

Every real service (Identity, Chat, etc.) will eventually need Postgres, Redis, and RabbitMQ running somewhere. Running them as real installed daemons on the dev machine is messy (version conflicts, manual setup, doesn't match how they'll actually run later). Docker Compose describes a set of containers as one declarative file and brings them all up/down together with one command - `docker compose up -d` / `docker compose down`, run from `deploy/`.

## Key pieces, and why they're there

**Healthchecks** (`healthcheck:` block per service) - a command Docker runs periodically inside the container to decide if the service is actually ready, not just "process started." Postgres' process can be up for a moment before it's actually accepting connections; `pg_isready` checks the real thing. This matters once other things (a startup script, another container with `depends_on: condition: service_healthy`) need to wait for "actually ready," not just "container started."

**Named volumes** (`postgres-data:`, `rabbitmq-data:`) - Docker containers are ephemeral by default; recreating a container throws away everything written inside it. A named volume is Docker-managed persistent storage that survives `docker compose down` (though not `docker compose down -v`, which deletes volumes too) - so local dev data (accounts, queued messages) doesn't vanish every time containers restart. Redis has no volume here since it's explicitly a cache/pub-sub layer, not source of truth - losing it is expected to be fine (see [go-workspaces.md](go-workspaces.md) is unrelated; see SYSTEM_DESIGN.md's Redis section for that reasoning).

**Published ports** (`"5432:5432"` etc.) - maps a container's internal port to the host machine's port, so processes running directly on the host (right now, every service - `go run ./services/identity`) can reach these containers via `localhost:5432` etc. Once services themselves get containerized (later phase), they'd instead talk to each other by *service name* (e.g. `postgres:5432`) over Compose's internal network, without needing published ports at all - published ports are purely a "let the host reach into the container" mechanism.

## What's deliberately not here yet

- Jaeger (added in the observability phase, once OpenTelemetry is wired into services)
- The services themselves as containers (no Dockerfiles exist yet - they still run via `go run` directly on the host during Phase 1)
- Any actual service code reading `POSTGRES_URL`/`REDIS_ADDR`/`RABBITMQ_URL` - that wiring happens as each service is built out; use those env var names for consistency with `pkg/config`'s style when it happens.
