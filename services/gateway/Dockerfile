# syntax=docker/dockerfile:1

# Build context is the repository root, not this directory - see
# services/identity/Dockerfile for why, and docs/ARCHITECTURE.md §9.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY . .
WORKDIR /src/services/gateway
RUN --mount=type=cache,target=/root/.cache/go-build \
	--mount=type=cache,target=/go/pkg/mod \
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gateway .

# distroless/static: no shell, no package manager, CA certificates bundled
# (confirmed by inspecting the image, not assumed) for the Turnstile and
# WebSocket-origin checks this service grows into, and a built-in
# non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gateway /gateway
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/gateway"]
