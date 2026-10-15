# Build Plan: Flamingo Chat

What is done, what is next, and in what order. Scope is defined in [PRD.md](PRD.md); reasons are in [decisions.jsonl](decisions.jsonl). Check an item off in the same pull request that completes it.

## Dates

| Date | Milestone |
|---|---|
| 2026-10-13 to 14 | Closed beta with a small group |
| 2026-10-15 | Public launch at KIIT, at the latest |
| 2026-12-15 to 18 | Move off Google Cloud |
| 2026-12-25 | Google Cloud trial ends |

If something has to slip, the launch date slips. Safety features and monitoring do not. (DEC-0024)

## Done

- [x] Monorepo scaffold: Go workspace, one module per service, proto contracts, buf configuration
- [x] Local dependencies with docker-compose
- [x] CI: build and test every module
- [x] Identity service: accounts, pseudonyms, badge verification
- [x] Shared gRPC request logging (`pkg/grpclog`)
- [x] Repository foundation: decision log, contributor documents, issue and pull request templates

## Planning still open

These are decided before the related work starts. (DEC-0016)

- [x] Frontend framework: React, Vite and TypeScript (DEC-0040). Screens are sketched in `docs/ARCHITECTURE.md` §10; a finished visual design is still open.
- [x] Architecture: how the services talk to each other, Kafka topics, what Redis holds - in `docs/ARCHITECTURE.md` (sections 4 to 7 and 11 are proposed, pending review; everything else is agreed)
- [ ] Infrastructure: cluster layout, cost against the trial credit, deploy pipeline (`docs/ARCHITECTURE.md` §12)
- [ ] Monitoring design (the stack and dashboards; code-level hooks are in `docs/ARCHITECTURE.md` §8)
- [x] License: Apache 2.0 (DEC-0035)
- [ ] Domain name
- [x] `docs/ARCHITECTURE.md`, replacing `SYSTEM_DESIGN.md` and `architecture/gateway.md`

## v1

Order within each group is set once the architecture is agreed.

### Repository

- [x] CI checks: formatting, `go vet`, race detector, linter, vulnerability scan, decision log validation (DEC-0051, DEC-0052)
- [x] CI: `buf lint`, passing now that the protocol redesign follows its naming rules
- [x] CI: `buf breaking`, enabled now that main has a non-breaking base again
- [x] Branch protection on `main`: pull requests only, CI required
- [x] Private vulnerability reporting enabled
- [x] Squash merging only, applied as a repository setting (DEC-0022)

### Local development

- [x] Tilt on a local kind cluster, replacing docker-compose (DEC-0039): `deploy/helm/service` (the shared chart), `deploy/k8s/dev` (Postgres, Redis, Kafka, the topic-creation Job), one `Dockerfile` per service, and the root `Tiltfile`. `tilt ci` brings every workload to healthy, confirmed against a real kind cluster, not only rendered - identity answers a real `grpcurl` call through a port-forward, and the three Kafka topics exist.
- [x] Wire in the delivery worker and the persistence worker now that both have a real consume loop (`protocol: worker` added to the shared chart for a service with no server of its own, DEC-0066)

### Backend

- [x] Identity: check the `hd` claim, key accounts on Google's `sub`, one-time gender, sign in to an existing account, delete inactive guests (INC-0006, DEC-0026 to DEC-0028)
- [x] Gateway: signup and sign-in over HTTP, session tokens, WebSocket connections, rate limits (DEC-0071 to DEC-0076)
- [x] Chat: rooms, membership, invite codes, sending messages, "keep chatting"
- [ ] Matching: random queue and gender-filtered queue, pairing, skip
- [ ] Presence: heartbeats and live counts
- [ ] Moderation: reports, blocks, bans, admin queue, gender change requests
- [ ] Kafka in place of RabbitMQ for durable events (DEC-0020)
- [x] Delivery worker: fans chat.events, matching.events and moderation.events out to Redis for live delivery (DEC-0067, DEC-0068)
- [x] Persistence worker: saved chat history and an analytics event log (DEC-0069)
- [ ] Admin command-line tool (DEC-0029)
- [ ] Log streaming gRPC calls as well as unary ones

### Frontend

- [ ] Web app in `web/`, deployed to Vercel (DEC-0032)
- [ ] Privacy policy and terms pages

### Infrastructure

- [x] Container images for every service that has one yet (the six gRPC/HTTP services, plus the delivery worker and the persistence worker)
- [ ] Kubernetes cluster on Google Cloud, defined as code (DEC-0023)
- [ ] Deploy pipeline from `main`
- [ ] Domain and HTTPS (DEC-0033)
- [ ] Google sign-in published to production, with brand verification

### Monitoring

Running before launch. (DEC-0024)

- [ ] Metrics and dashboards for every service
- [ ] Distributed tracing across services and the message broker
- [ ] Central logs
- [ ] Alerts
- [ ] Budget alert on the Google Cloud billing account

### Before launch

- [ ] Load test, with the method and results committed (DEC-0025)
- [ ] Security review
- [ ] Sign-in re-tested with a KIIT account after publishing to production (DEC-0027)
- [ ] Closed beta

## After launch

- [ ] Benchmarks with documented method and saved results
- [ ] Product measures from the PRD, reported from stored data
- [ ] Migration off Google Cloud

## Later

- [ ] Group discovery
- [ ] Identity reveal mechanic
