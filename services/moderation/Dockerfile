# syntax=docker/dockerfile:1

# Build context is the repository root, not this directory - see
# services/identity/Dockerfile for why, and docs/ARCHITECTURE.md §9.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY . .
WORKDIR /src/services/moderation
RUN --mount=type=cache,target=/root/.cache/go-build \
	--mount=type=cache,target=/go/pkg/mod \
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/moderation .

# distroless/static: no shell, no package manager, CA certificates bundled
# and a built-in non-root user (confirmed by inspecting the image, not
# assumed from its docs).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/moderation /moderation
USER nonroot:nonroot
EXPOSE 50055
ENTRYPOINT ["/moderation"]
