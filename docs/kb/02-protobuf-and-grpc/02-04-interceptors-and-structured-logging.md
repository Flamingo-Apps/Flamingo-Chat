# gRPC interceptors and structured logging

## The gap this fills

Before this, Identity's `main.go` had a handful of `log.Printf` calls for startup events only - nothing logged per request. If a call failed in the field, there'd be no record of it beyond whatever the client itself saw. This is the standard first thing a real service gets once it has any real logic: a log line per request, at the one place every request already passes through.

## Interceptors are gRPC's middleware

Same concept as Express middleware in Node, or an HTTP handler wrapper in any framework: a function that wraps every call to add cross-cutting behavior without touching the business logic itself. gRPC calls this an **interceptor**. A *unary* interceptor wraps single request/response RPCs (as opposed to streaming ones):

```go
type UnaryServerInterceptor func(
    ctx context.Context,
    req any,
    info *grpc.UnaryServerInfo,
    handler grpc.UnaryHandler,
) (resp any, err error)
```

It receives the request, a `handler` function that *is* the actual RPC method, and is expected to call `handler(ctx, req)` itself and return its result - anything done before or after that call is the "wrapping." Registered once, for the whole server:

```go
srv := grpc.NewServer(grpc.UnaryInterceptor(grpclog.UnaryServerInterceptor(logger)))
```

## Why `log/slog`, not `log.Printf`

`log.Printf("verify badge failed for %s: %v", id, err)` produces a sentence - fine for a human reading a terminal, useless for a machine trying to answer "show me every failed request for account X" across thousands of lines. `log/slog` (standard library since Go 1.21) produces structured key-value pairs instead:

```go
logger.ErrorContext(ctx, "grpc request", "method", info.FullMethod, "code", "NotFound", "err", err.Error())
// {"time":"...","level":"ERROR","msg":"grpc request","method":"...","code":"NotFound","err":"..."}
```

That JSON shape is what makes swapping the *destination* later (stdout now, shipped to Loki/Datadog/whatever in Phase 3) a config change, not a rewrite of every log call site across every service.

## Where this lives, and why

`pkg/grpclog` (not inside `services/identity`), because every service will eventually want the exact same interceptor - it's genuinely shared infrastructure, not Identity-specific. `services/identity/main.go` is the first to wire it in since it's the first service with real RPCs worth logging; other services adopt `grpclog.UnaryServerInterceptor(logger)` the same way once they have logic to log.

## Why this is "forward-compatible with Phase 3," concretely

`SYSTEM_DESIGN.md` §6 calls for metrics (Prometheus), tracing (OpenTelemetry/Jaeger), and structured logging - three different concerns, but all three attach at exactly this same wrapping point: before/after the `handler(ctx, req)` call, with access to the same `duration` this interceptor already computes and the same `status.Code(err)` it already extracts. Building the interceptor now means Phase 3 adds a span and a metrics recorder to the same function, rather than inventing where in the codebase that logic should even live.

## Testing an interceptor

`pkg/grpclog/interceptor_test.go` doesn't need a real gRPC server or network connection at all - a `UnaryServerInterceptor` is just a function, so tests call it directly with a hand-built `*grpc.UnaryServerInfo` and a fake `handler` closure, then assert on both the passed-through response/error and the JSON written to an in-memory buffer (via `slog.NewJSONHandler(buf, nil)` instead of `os.Stdout`). Same "test the interface, not the framework" instinct as [01-04-interfaces-for-testability.md](../01-go-fundamentals/01-04-interfaces-for-testability.md).
