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

Postgres is the source of truth. Redis carries live message delivery and presence. Kafka will carry durable events between services; the local setup still runs RabbitMQ until that change lands. The web frontend will live in `web/`.

The design is described in [docs/SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md), and the reasons behind each choice are in [docs/decisions.jsonl](docs/decisions.jsonl).

## Run it locally

You need Go 1.24.3 or newer and Docker with Compose.

```sh
# start Postgres, Redis and the broker
cd deploy && docker compose up -d && cd ..

# run the identity service; it applies its database migrations on startup
cd services/identity && go run .
```

The identity service listens for gRPC on port 50051. With [grpcurl](https://github.com/fullstorydev/grpcurl) installed you can call it directly:

```sh
grpcurl -plaintext -d '{"pseudonym": "shadow_fox"}' \
  localhost:50051 identity.v1.IdentityService/CreateAccount
```

Each service is its own Go module, so run tests from inside a module:

```sh
cd services/identity && go test ./...
```

## Documentation

- [docs/PRD.md](docs/PRD.md): what is being built, for whom, and what is out of scope
- [docs/SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md): services, data flows and contracts
- [docs/PLAN.md](docs/PLAN.md): build order and progress
- [docs/decisions.jsonl](docs/decisions.jsonl): decision and incident log

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup and the pull request workflow, and [AGENTS.md](AGENTS.md) if you work with an AI coding agent. Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

To report a security problem, follow [SECURITY.md](SECURITY.md). Please do not open a public issue for it.

## License

Flamingo Chat is licensed under the [Apache License, Version 2.0](LICENSE).
