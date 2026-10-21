# syntax=docker/dockerfile:1

# Build context is the repository root, not this directory - see
# services/identity/Dockerfile for why, and docs/ARCHITECTURE.md §9.
FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY . .
WORKDIR /src/services/presence
RUN --mount=type=cache,target=/root/.cache/go-build \
	--mount=type=cache,target=/go/pkg/mod \
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/presence .

# distroless/static: no shell, no package manager, CA certificates bundled
# and a built-in non-root user (confirmed by inspecting the image, not
# assumed from its docs).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/presence /presence
USER nonroot:nonroot
EXPOSE 50054
ENTRYPOINT ["/presence"]
