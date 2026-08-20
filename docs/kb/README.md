# Knowledge Base

Running notes on distributed-systems concepts as we hit them while building Flamingo Chat. Each file is one topic, written for the "why does this exist and how does it fit here" level - not a full tutorial, just enough to remember the concept and how it shows up in this repo.

Add a new file per topic (not per session). If a later session deepens an existing topic, extend that file rather than starting a new one.

## Topics

- [protobuf-and-buf.md](protobuf-and-buf.md) - Protocol Buffers, gRPC contracts, buf codegen
- [go-workspaces.md](go-workspaces.md) - multi-module monorepos with `go.work`
- [go-modules-and-toolchains.md](go-modules-and-toolchains.md) - MVS, the `go` directive vs. toolchain, pinning dependency versions
- [docker-compose-for-local-dev.md](docker-compose-for-local-dev.md) - healthchecks, named volumes, published ports vs. service-name networking
