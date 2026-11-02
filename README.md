# Flamingo Chat

Anonymous chat for one campus: group rooms, random 1:1 matching, and gender-filtered matching for verified students.

> **Status: running as a pilot, closed beta not started.** All eight backend services and the web app are built and deployed to a single-node Google Kubernetes Engine cluster. It has not been launched to students yet; what is done and what is left before that is in [docs/PLAN.md](docs/PLAN.md), including a list of known defects that each have a failing test.

## What it is

Students want a low-pressure way to talk to people on their campus they do not already know. Flamingo Chat lets anyone join as a guest with a made-up name, chat in group rooms and get matched with a random person. Signing in with a college Google account unlocks matching by gender. Other users only ever see a pseudonym.

This is a rebuild of an earlier prototype that reached about 400 users in its first two days and then lost them. This version is also built as a distributed system on purpose, to learn how one is designed, deployed and measured, and the measuring is part of the project: results are committed with their method, and nothing is quoted without a dated artefact behind it.

## What it can take

A load test on the production node, run on 28 October 2026, with the generator inside the cluster. Simulated students sign up as guests, get matched with a stranger and send a message every 3 seconds. Latency is the time for a message to reach the other person.

| Simulated users | p95 message delivery |
|---|---|
| 25 | 61 ms |
| 200 | 181 to 299 ms (two runs) |
| 275 | 578 ms |
| 300 | 719 ms in one run, 5.5 s in another: the node's CPU ran out |

The whole system ran on **one e2-standard-2 virtual machine (2 vCPU, 8 GB)**, with the load generator on it too. The limit was the CPU, not memory, disks or any single service, and the same level gave very different results near the limit, so these are ranges, not a capacity figure. They are simulated users at a steady rate, not real students, and the path through Cloudflare was not included. The machine, versions, method, every result and the caveats are in [loadtest/results/](loadtest/results/) and [DEC-0110](docs/decisions.jsonl). The results describe commit `b093bac`; later changes add some work per user, so they need a re-run before being quoted for the current release.

## How it is built

The backend is a set of Go services that talk to each other over gRPC. Only the gateway is reachable from outside.

```
browser ──▶ Vercel                      the React app's files
browser ──HTTPS and WebSocket──▶ gateway ──gRPC──▶ identity, chat, matching, presence, moderation

a message:  chat ──▶ Kafka ──▶ delivery-worker ──▶ Redis pub/sub ──▶ gateway ──▶ the other person's browser
                       └─────▶ persistence-worker ──▶ Postgres (history)
```

| Service | Responsibility |
|---|---|
| `gateway` | Public HTTP and WebSocket entry point, sessions, rate limits, routing |
| `identity` | Accounts, pseudonyms, Google sign-in and college verification |
| `chat` | Rooms, membership, invite codes, sending messages |
| `matching` | The match queue and pairing (an atomic Redis script) |
| `presence` | Live online counts |
| `moderation` | Reports, blocks, bans |
| `delivery-worker` | Reads chat events from Kafka and delivers them live through Redis |
| `persistence-worker` | Reads chat events from Kafka and writes history to Postgres |

Postgres is the source of truth. Redis carries live message delivery and the matching queue. Kafka carries durable events between services. The web app in `web/` is React, Vite and TypeScript. It runs on Vercel and talks only to the gateway.

It is deployed with one Helm chart for every service, behind a Cloudflare Tunnel, with Prometheus, Grafana and Loki running in the same cluster. The design is described in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), and the reasons behind each choice, and every incident, are in [docs/decisions.jsonl](docs/decisions.jsonl).

## How it is tested

- Unit tests for each service against in-memory fakes, and integration tests against a real Postgres and Redis.
- A browser suite of 34 checks, `web/e2e/blackbox.mjs`, that drives the real web app against the real local stack: two people chatting, a reload in the middle of a chat, the gateway restarting, a server that cannot be reached, two tabs, a dropped connection. The checks added for each bug found in use were run against the code before the fix, and fail there.
- A load test, [loadtest/](loadtest/), run inside the cluster.
- Known defects are not hidden: each has a failing test under the build tag `defect`, and `scripts/run-defect-tests.sh` lists which are still open ([CONTRIBUTING.md](CONTRIBUTING.md), "Tests for known defects").

## Run it locally

Everything runs on a local Kubernetes cluster through [Tilt](https://tilt.dev), using the same manifests and Helm chart that deploy to the real cluster; see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) section 9.

You need Go 1.27.2 or newer, Docker, [kind](https://kind.sigs.k8s.io), and [Tilt](https://docs.tilt.dev/install.html).

```sh
# one-time: create the local cluster Tilt deploys into
kind create cluster --name flamingo-dev --config deploy/k8s/dev/kind-config.yaml

# build every service, deploy Postgres/Redis/Kafka and every service, and
# keep rebuilding on changes. Opens a UI at localhost:10350.
tilt up

# or, for a one-shot build-and-verify with no UI (what CI would run):
tilt ci
```

`tilt up` forwards each service's port to localhost: 8080 for the gateway, 50051 for identity, 50052 for matching, 50053 for chat, 50054 for presence, 50055 for moderation, 5432 for Postgres, 6379 for Redis, and 9092 for Kafka. With [grpcurl](https://github.com/fullstorydev/grpcurl) installed you can call identity directly:

```sh
grpcurl -plaintext -d '{"pseudonym": "shadow_fox"}' \
  localhost:50051 identity.v1.IdentityService/CreateAccount
```

It also runs the web app at [localhost:8000](http://localhost:8000) and a local monitoring stack (DEC-0095), each linked from its resource in the Tilt UI:

- [localhost:3000/d/flamingo-overview](http://localhost:3000/d/flamingo-overview): Grafana's Flamingo dashboard (traffic, errors, latency, Kafka consumer lag, logs). The login is printed in the `grafana` resource's log in Tilt. Dashboards live as JSON in `deploy/observability/dashboards/` and load automatically.
- [localhost:9090/targets](http://localhost:9090/targets): Prometheus, and whether it is scraping every service. With more than 20 scrape pools its UI shows only the first one, under a "Found many pools, showing only one" banner; pick others from the dropdown, or run the query `up` to see every target at once (1 = being scraped).
- [localhost:12345](http://localhost:12345): Alloy's pipeline graph, the agent shipping every pod's logs to Loki.

To work on the web app without the whole stack, run `npm run dev:mock` and `npm run dev` in `web/`. To run the browser suite against the local stack, run `npm run test:blackbox` in `web/`; it refuses to run against anything but localhost.

`go test -tags e2e ./tests/e2e/...` is the quickest way to put real traffic on the dashboard. The gRPC services start every request counter at zero, so even one run shows up; gateway's HTTP counters only appear on their first request, so its panels need a second run.

Each service is its own Go module, so build and test it directly, without Tilt, from inside its directory:

```sh
cd services/identity && go build ./... && go test ./...
```

## Documentation

- [docs/PRD.md](docs/PRD.md): what is being built, for whom, and what is out of scope
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): services, data flows and contracts
- [docs/PLAN.md](docs/PLAN.md): build order and progress
- [docs/decisions.jsonl](docs/decisions.jsonl): decision and incident log
- [loadtest/results/](loadtest/results/): the load test results, the machine they ran on, and the measurements

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup and the pull request workflow, and [AGENTS.md](AGENTS.md) if you work with an AI coding agent. Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

To report a security problem, follow [SECURITY.md](SECURITY.md). Please do not open a public issue for it.

## License

Flamingo Chat is licensed under the [Apache License, Version 2.0](LICENSE).
