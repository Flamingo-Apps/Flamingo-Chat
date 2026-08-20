# Knowledge Base

Running notes on distributed-systems concepts as we hit them while building Flamingo Chat. Organized by topic, one numbered folder per subject area, one numbered article per concept - written for the "why does this exist and how does it fit here" level, not a full tutorial.

**Adding to this**: if a new article fits an existing topic folder, number it the next `0X` in that folder. If it's a genuinely new subject area, add a new `0N-topic-name/` folder at the end and update the index below. Don't renumber existing folders/articles just to make room - append instead.

## 01. Go fundamentals

- [01-01-gofmt-and-go-vet.md](01-go-fundamentals/01-01-gofmt-and-go-vet.md) - formatting and static analysis
- [01-02-go-workspaces.md](01-go-fundamentals/01-02-go-workspaces.md) - multi-module monorepos with `go.work`
- [01-03-modules-and-toolchains.md](01-go-fundamentals/01-03-modules-and-toolchains.md) - MVS, the `go` directive vs. toolchain, pinning dependency versions
- [01-04-interfaces-for-testability.md](01-go-fundamentals/01-04-interfaces-for-testability.md) - the repository pattern, fakes instead of mocks

## 02. Protobuf and gRPC

- [02-01-protobuf-and-buf.md](02-protobuf-and-grpc/02-01-protobuf-and-buf.md) - Protocol Buffers, gRPC contracts, buf codegen
- [02-02-grpc-error-codes.md](02-protobuf-and-grpc/02-02-grpc-error-codes.md) - status/codes, mapping domain errors to wire errors
- [02-03-grpcurl.md](02-protobuf-and-grpc/02-03-grpcurl.md) - calling a gRPC service by hand via reflection
- [02-04-interceptors-and-structured-logging.md](02-protobuf-and-grpc/02-04-interceptors-and-structured-logging.md) - gRPC middleware, log/slog, the shared `pkg/grpclog` package

## 03. Databases

- [03-01-postgres-and-pgx.md](03-databases/03-01-postgres-and-pgx.md) - pgx/pgxpool, translating Postgres errors
- [03-02-schema-migrations.md](03-databases/03-02-schema-migrations.md) - golang-migrate, embedded `.sql` files

## 04. Auth and verification

- [04-01-oauth-and-oidc.md](04-auth-and-verification/04-01-oauth-and-oidc.md) - verifying college email via an OAuth ID token

## 05. Infra and devops

- [05-01-docker-compose.md](05-infra-and-devops/05-01-docker-compose.md) - healthchecks, named volumes, published ports vs. service-name networking
- [05-02-github-actions-ci.md](05-infra-and-devops/05-02-github-actions-ci.md) - workflow/job/step/action, matrix strategy, why per-module builds
