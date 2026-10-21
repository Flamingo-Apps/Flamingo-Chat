# syntax=docker/dockerfile:1

# Build context is the repository root, not this directory. go.work ties
# every module together; building this service in isolation would need
# either a trimmed go.work or replace directives duplicating what go.work
# already provides, so the whole source tree is copied in instead. See
# docs/ARCHITECTURE.md §9.
FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY . .
WORKDIR /src/services/identity
RUN --mount=type=cache,target=/root/.cache/go-build \
	--mount=type=cache,target=/go/pkg/mod \
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/identity .

# distroless/static has no shell and no package manager - exactly what a
# statically linked Go binary needs, and nothing more. Confirmed by
# inspecting the image directly (not assumed from its docs): it bundles
# /etc/ssl/certs/ca-certificates.crt, which identity needs to verify
# Google's OIDC discovery and JWKS endpoints over HTTPS, and it runs as a
# built-in non-root user by default.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/identity /identity
USER nonroot:nonroot
EXPOSE 50051
ENTRYPOINT ["/identity"]
