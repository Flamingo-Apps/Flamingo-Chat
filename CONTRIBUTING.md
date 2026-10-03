# Contributing to Flamingo Chat

This guide covers how to set up the project, how changes get merged, and the few project-specific rules. If you work with an AI coding agent, also read [AGENTS.md](AGENTS.md).

## Before you start

- Read the [README](README.md), then [docs/PRD.md](docs/PRD.md) for what is being built and [docs/PLAN.md](docs/PLAN.md) for what is next.
- For anything larger than a small fix, open an issue first and describe what you want to change. Design changes are discussed and agreed before they are built.
- Taking part in this project means following the [Code of Conduct](CODE_OF_CONDUCT.md).
- Report security problems privately, as described in [SECURITY.md](SECURITY.md). Never open a public issue for one.

## Local setup

You need:

- Go 1.24.3 or newer. CI builds with 1.24.3, so do not add a dependency that requires a newer version.
- Docker with Compose, for Postgres, Redis and the message broker.
- [buf](https://buf.build), only if you change `.proto` files.

```sh
git clone https://github.com/Flamingo-Apps/Flamingo-Chat.git
cd Flamingo-Chat

# start local dependencies
cd deploy && docker compose up -d && cd ..

# build and test one module
cd services/identity
go build ./...
go test ./...
```

Each service is its own Go module. Build and test from inside the module's directory; `go build ./...` from the repository root does not cover the modules.

## Workflow

The project uses [GitHub Flow](https://docs.github.com/en/get-started/using-github/github-flow). Nothing is committed directly to `main`.

1. **Create a branch** from `main`, named `<type>/<short-description>`, for example `feat/gateway-signup`, `fix/identity-malformed-id` or `docs/architecture`.
2. **Make your change**, with tests. Keep a pull request to one concern.
3. **Open a pull request** and fill in the template. Explain what changed and why.
4. **CI must pass.** It builds and tests every Go module.
5. **Merge.** Pull requests are squash-merged, so each one becomes a single commit on `main`.
6. **Delete the branch.**

### Pull request titles

The title becomes the commit message on `main`, so it follows [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):

```
<type>[optional scope]: <description>
```

Commit messages carry no AI attribution: no `Co-Authored-By` trailer for an AI tool and no "generated with" line.

Types used here: `feat`, `fix`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`, `chore`. The scope is usually the service or area, for example `feat(gateway): add signup endpoint`. Mark a breaking change with `!` after the type or scope.

## Changing `.proto` files

Every service depends on the generated code in `proto/gen/go`, so a contract change affects all of them.

- Keep changes minimal and additive. Do not renumber or reuse field numbers.
- Run `buf generate` from the repository root and commit the generated code together with the `.proto` change.
- Say clearly in the pull request that the contract changed.

## Tests

- Business logic is tested against in-memory fakes of its storage and provider interfaces. See `services/identity/internal/server/server_test.go`.
- Fakes do not reproduce real database behaviour. Code that talks to a database also needs a test against the real one.

## Decision log

[docs/decisions.jsonl](docs/decisions.jsonl) records why the project is the way it is. Each line is one JSON object: either a decision, written like an architecture decision record, or an incident, written like a short postmortem.

Add an entry in the same pull request when:

- you choose between real alternatives in a way that would be costly to reverse, or that someone will later ask "why?" about; or
- something broke, was wrong or cost time, and there is a lesson in it.

Rules:

- Append new lines at the end. Give the entry the next free number for its type (`DEC-NNNN` or `INC-NNNN`). Never renumber.
- Do not rewrite old entries. The two permitted edits are marking a decision as superseded (`"status": "superseded"` plus `"superseded_by"`) and closing an open incident (`"status": "resolved"` plus its `resolution`).
- A decision that replaces an earlier one names it in `supersedes`.
- Dates are `YYYY-MM-DD`. `logged_at` is the UTC time the entry was written.

Decision fields:

| Field | Meaning |
|---|---|
| `id`, `type`, `date` | `DEC-NNNN`, `"decision"`, the date it was decided |
| `status` | `accepted`, `superseded` or `proposed` |
| `area` | Short topic, such as `identity`, `messaging`, `infra`, `process` |
| `title` | The decision in one line |
| `context` | The situation that forced a choice |
| `options` | The alternatives that were considered |
| `decision` | What was chosen |
| `rationale` | Why |
| `tradeoffs` | What it costs or gives up |
| `supersedes`, `superseded_by` | Optional links to other decisions |
| `refs` | Commits, files, pull requests or source URLs |
| `logged_at` | UTC timestamp |

Incident fields: `id` (`INC-NNNN`), `type` (`"incident"`), `date`, `status` (`open` or `resolved`), `severity` (`low`, `medium`, `high`), `area`, `title`, `what_happened`, `impact`, `root_cause`, `resolution`, `lessons`, `refs`, `logged_at`.

Useful commands:

```sh
# list accepted decisions
jq -r 'select(.type=="decision" and .status=="accepted") | "\(.id)  \(.title)"' docs/decisions.jsonl

# read one entry
jq 'select(.id=="DEC-0020")' docs/decisions.jsonl

# check that every line is valid JSON
jq -e . docs/decisions.jsonl > /dev/null && echo ok
```

## Documentation

The documentation is kept deliberately small: this file, the README, and the PRD, design, plan and decision log under `docs/`. Please do not add new documentation files without agreeing it in an issue first.

## Reporting bugs and requesting features

Use the issue templates. A good bug report says what you did, what you expected and what happened instead, with logs or steps to reproduce.
