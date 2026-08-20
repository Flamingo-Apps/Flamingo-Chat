# Protocol Buffers & buf

## The problem

Services in this system talk to each other over gRPC. For that to work, both sides (Go processes, possibly different services owned by different teams later) need to agree on exactly what functions exist and what shape the data is. Protocol Buffers (protobuf) is the language used to write that agreement down, in a way that isn't tied to Go specifically.

## The pieces

**`.proto` files** (`proto/identity/v1/identity.proto` etc.) - hand-written contract files. Two kinds of blocks matter:
- `message` - a data shape (fields, types, field numbers)
- `service` - a set of RPCs, each with a request message type and a response message type

This is the file you edit when a service's API changes. It's the source of truth other services and generated code are built from.

**Codegen** - a `.proto` file alone isn't code. A generator reads it and emits real Go structs (for messages) and Go interfaces + client/server plumbing (for services). `buf` is the tool that runs this generation; it wraps `protoc` (the original Google compiler) with saner config and dependency management.

**`buf.yaml`** - configures buf for the whole `proto/` tree: where the module root is, which lint and breaking-change rules apply. Think of it as buf's project config.

**`buf.gen.yaml`** - configures what to *generate* and *where*. It lists plugins (e.g. `protoc-gen-go` for message structs, `protoc-gen-go-grpc` for service interfaces) and an output path. Running `buf generate` reads this + `buf.yaml` + the `.proto` files and writes generated `.go` files into `proto/gen/go/`.

**`proto/gen/go/`** - the generated output. Never hand-edit these files - they get overwritten every time `buf generate` runs. This is what service code actually imports to get typed structs and gRPC client/server interfaces, instead of hand-writing serialization.

## Workflow when a contract changes

1. Edit the relevant `.proto` file (add a field, add an RPC, etc.)
2. Run `buf generate` from wherever `buf.yaml` lives
3. Commit both the `.proto` change and the regenerated code together
4. Per `CLAUDE.md`: proto changes are the one thing that isn't isolated to one session/branch - every service depends on `proto/gen/go`, so flag proto changes clearly and keep them additive where possible.

## Open questions / things to dig into later

- Field numbering rules (why you never reuse or reorder a field number once it's shipped) - matters once this has real traffic and can't just be redeployed with breaking changes.
- `buf breaking` - a lint step that can catch accidental breaking changes to a contract in CI, not set up yet.
