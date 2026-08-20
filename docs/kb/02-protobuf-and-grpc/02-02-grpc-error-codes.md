# gRPC error codes (status/codes)

## The problem with returning a plain Go error

If a gRPC service method returns a plain `error`, the client just sees an opaque failure. gRPC has its own standard vocabulary of failure reasons - `NotFound`, `AlreadyExists`, `InvalidArgument`, `PermissionDenied`, `Internal`, and others - defined once so any gRPC client, in any language, can branch on *why* a call failed without parsing an error string.

## The two packages

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

return nil, status.Error(codes.NotFound, "account not found")
return nil, status.Errorf(codes.Internal, "create account: %v", err)
```

`codes` is just the enum of standard reasons. `status.Error`/`status.Errorf` wrap a code and a message into something that satisfies Go's normal `error` interface but also carries the structured code over the wire.

## How `server.go` uses this

Every method in `services/identity/internal/server/server.go` maps a specific failure to a specific code, never a bare `error`:

| Situation | Code |
|---|---|
| Empty pseudonym | `InvalidArgument` |
| Pseudonym/email already taken | `AlreadyExists` |
| Account ID doesn't exist | `NotFound` |
| Token doesn't verify, or gender unspecified | `InvalidArgument` |
| Email verified but wrong domain | `PermissionDenied` |
| Anything else (a real DB error) | `Internal` |

A client (or, in tests, `status.Code(err)`) can check the code without string-matching the message. `server_test.go` does exactly this: `if status.Code(err) != codes.NotFound { ... }`.

## Checking a code from the caller's side

```go
if status.Code(err) == codes.NotFound {
    // handle missing account
}
```

This is also why the store layer (`internal/store`) has its own separate sentinel errors (`ErrNotFound`, `ErrAlreadyExists`) rather than returning gRPC status codes directly - the store shouldn't know it's being used by a gRPC service at all, it's `server.go`'s job to translate a plain Go error from the store into the right wire-level code. Same "depend on the boundary, not the concrete detail" idea as [../01-go-fundamentals/01-04-interfaces-for-testability.md](../01-go-fundamentals/01-04-interfaces-for-testability.md).
