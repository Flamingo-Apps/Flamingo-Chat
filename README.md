# Flamingo Chat

Anonymous chat for one campus: group rooms, random 1:1 matching, and gender-filtered matching for verified students.

> **Status: in development, not launched.** The identity service works; the other services are skeletons. See [docs/PLAN.md](docs/PLAN.md) for what is done and what is next.

## What it is

Students want a low-pressure way to talk to people on their campus they do not already know. Flamingo Chat lets anyone join with a pseudonym, chat in group rooms and get matched with a random person. Signing in with a college account unlocks matching by gender. Other users only ever see a pseudonym.

This is a rebuild of an earlier prototype that reached about 400 users in its first two days and then lost them. This version is also built as a distributed system on purpose, to learn how one is designed, deployed and measured.

## How it is built

The backend is a set of Go services that talk to each other over gRPC. Only the gateway is reachable from outside.

| Service | Responsibility | State |
|---|---|---|
| `gateway` | Public HTTP and WebSocket entry point, sessions, routing | Skeleton |
| `identity` | Accounts, pseudonyms, college verification | Implemented |
| `chat` | Rooms, membership, invite codes, messages | Skeleton |
| `matching` | Match queues and pairing | Skeleton |
| `presence` | Live online counts | Skeleton |
| `moderation` | Reports, blocks, bans | Skeleton |
| `persistence-worker` | Writes chat history to the database | Skeleton |

Postgres is the source of truth. Redis carries live message delivery and presence. Kafka carries durable events between services. The web frontend will live in `web/`.

The design is described in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), and the reasons behind each choice are in [docs/decisions.jsonl](docs/decisions.jsonl).

## Run it locally

Everything runs on a local Kubernetes cluster through [Tilt](https://tilt.dev), using the same manifests and Helm chart that deploy to the real cluster - see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §9.

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

Each service is its own Go module, so build and test it directly, without Tilt, from inside its directory:

```sh
cd services/identity && go build ./... && go test ./...
```

## Documentation

- [docs/PRD.md](docs/PRD.md): what is being built, for whom, and what is out of scope
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): services, data flows and contracts
- [docs/PLAN.md](docs/PLAN.md): build order and progress
- [docs/decisions.jsonl](docs/decisions.jsonl): decision and incident log

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup and the pull request workflow, and [AGENTS.md](AGENTS.md) if you work with an AI coding agent. Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

To report a security problem, follow [SECURITY.md](SECURITY.md). Please do not open a public issue for it.

## License

Flamingo Chat is licensed under the [Apache License, Version 2.0](LICENSE).
