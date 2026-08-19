# Build Plan: Flamingo Chat

Priority list, phase by phase. Edit freely - insert/reorder items if priority changes, that's the point of this doc. Full detail on any item lives in [SYSTEM_DESIGN.md](SYSTEM_DESIGN.md).

## Phase 0 - Setup

- [ ] Monorepo scaffold (go.work, services/, proto/, buf config)
- [ ] docker-compose for local dev (Postgres, Redis, RabbitMQ)
- [ ] Basic CI (build + test on push)

## Phase 1 - Core walking skeleton (real-time path only, no durability yet)

- [ ] Identity Service (accounts, badge verification, pseudonym)
- [ ] API Gateway (HTTP/WebSocket, JWT, routing)
- [ ] Chat Service (rooms, messages, invite codes)
- [ ] Matching Service (gender-wise queue + pairing)
- [ ] Presence Service (live online counts)

Note: no durable chat history until Phase 2 - messages only flow live (Redis pub/sub), nothing persisted to Postgres yet. Deliberate tradeoff to get the real-time path working end to end first.

## Phase 2 - Durability & safety

- [ ] Persistence Worker (durable chat history)
- [ ] Moderation Service (reports, blocks)

## Phase 3 - Observability

- [ ] Prometheus + Grafana
- [ ] OpenTelemetry + Jaeger/Tempo tracing, end to end
- [ ] Structured logging (+ Loki, optional)

## Phase 4 - Infra / deployment

- [ ] k3s cluster (VPS nodes)
- [ ] CI/CD deploy pipeline (build, push to ghcr.io, deploy)
- [ ] TLS + domain

## Phase 5 - Benchmarking (for resume-worthy numbers)

- [ ] Load test WebSocket concurrency
- [ ] Load test message throughput/latency
- [ ] Record real p50/p95/p99 numbers with documented methodology

## Phase 6 - Product expansion (deferred features, revisit later)

- [ ] Group search/discovery
- [ ] Phase 2 dating/reveal mechanic (see [CONCEPT.md](CONCEPT.md))
