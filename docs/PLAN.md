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

- [ ] Screens and flows for the web app, and the frontend framework
- [ ] Architecture: how the services talk to each other, Kafka topics, what Redis holds
- [ ] Infrastructure: cluster layout, cost against the trial credit, deploy pipeline
- [ ] Monitoring design
- [x] License: Apache 2.0 (DEC-0035)
- [ ] Domain name
- [ ] `docs/ARCHITECTURE.md`, replacing `SYSTEM_DESIGN.md` and `architecture/gateway.md`

## v1

Order within each group is set once the architecture is agreed.

### Repository

- [ ] CI checks: formatting, `go vet`, race detector, linter, vulnerability scan, proto lint and breaking-change check, decision log validation
- [ ] Branch protection on `main`: pull requests only, CI required
- [ ] Private vulnerability reporting enabled

### Backend

- [ ] Identity: check the `hd` claim, key accounts on Google's `sub`, one-time gender, sign in to an existing account, delete inactive guests (INC-0006, DEC-0026 to DEC-0028)
- [ ] Gateway: signup and sign-in over HTTP, session tokens, WebSocket connections, rate limits
- [ ] Chat: rooms, membership, invite codes, sending messages, "keep chatting"
- [ ] Matching: random queue and gender-filtered queue, pairing, skip
- [ ] Presence: heartbeats and live counts
- [ ] Moderation: reports, blocks, bans, admin queue, gender change requests
- [ ] Kafka in place of RabbitMQ for durable events (DEC-0020)
- [ ] Persistence worker: saved chat history
- [ ] Admin command-line tool (DEC-0029)
- [ ] Log streaming gRPC calls as well as unary ones

### Frontend

- [ ] Web app in `web/`, deployed to Vercel (DEC-0032)
- [ ] Privacy policy and terms pages

### Infrastructure

- [ ] Container images for every service
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
