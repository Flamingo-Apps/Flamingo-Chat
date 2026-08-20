# grpcurl

## What it's for

`grpcurl` is `curl` for gRPC. Normally, calling a gRPC service means writing a real client in some language, generated from the `.proto` file. `grpcurl` skips that: it's a standalone CLI that can call any gRPC method directly, as long as the server has reflection enabled (every service's `main.go` in this repo already calls `reflection.Register(srv)`, so this works out of the box against every service here).

## Install

```sh
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

Installs to `$(go env GOPATH)/bin` (usually `~/go/bin`) - make sure that's on `PATH`.

## Usage

```sh
# list every service the server exposes (via reflection)
grpcurl -plaintext localhost:50051 list

# call a method, request body as JSON
grpcurl -plaintext -d '{"pseudonym": "shadow_fox"}' \
  localhost:50051 identity.v1.IdentityService/CreateAccount
```

`-plaintext` disables TLS, since local dev services aren't running with certificates. The JSON body's field names use the proto's `json_name` (camelCase by default: `account_id` in the `.proto` becomes `account_id` or `accountId` in JSON, grpcurl accepts both).

## Why this mattered for Identity

Unit tests (`server_test.go`) prove the business logic is correct against fakes, but they can't catch bugs that only show up against a *real* Postgres - and one did: `GetAccount` with a malformed (non-UUID) ID was returning `Internal` with a leaked database error message instead of `NotFound`, because Postgres rejects a bad UUID literal before the query even runs, a path the fake store couldn't reproduce. Running the real service against real Postgres and poking it with `grpcurl` surfaced that immediately - see the fix in `store/postgres.go`'s `isNotFound` helper.

This is the manual-testing equivalent of what `docker compose up` + `psql` gave for verifying `deploy/docker-compose.yml` in Phase 0 - proving something behaves correctly by actually running it, not just trusting the code.
