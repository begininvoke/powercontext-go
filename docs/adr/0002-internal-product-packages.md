# ADR 0002: Keep product-only domains internal

- Status: accepted
- Compatibility target: Python `3a6cb0151670eaff7dc0293466edd673124e80da`
- Decision date: 2026-08-27

## Context

The initial Go migration exposed nearly every domain-shaped package at the
module root. That made `review`, `contextpack`, `handoffreport`, `stats`,
`work`, and `runtime` look like supported embedded-SDK contracts even though
all of their consumers are inside this repository. Host integrations use the
HTTP contract, and the public Go client uses generated OpenAPI types.

The root `powercontext.PowerContext` generic only stored three values and had
no runtime, tests, examples, or consumers. It did not compose the application
or enforce a domain invariant. Keeping these accidental APIs would make
internal evolution depend on compatibility promises the product does not use.

## Decision

Product-only domains move beneath `internal`:

- `internal/review`
- `internal/contextpack`
- `internal/handoffreport`
- `internal/stats`
- `internal/work`
- `internal/runtime`

The embedded seekDB loader and sqlite-vec extension move beneath their owning
relational adapter as `internal/sqlstore/seekdb` and
`internal/sqlstore/sqlitevec`. The empty root package and
`server.Application.Runtime` escape hatch are removed.

The deliberate public Go surface remains:

- `api/v1` and `client` for the wire contract and remote client;
- `server` for process composition;
- `source`, `artifact`, `trigger`, and `inference` for lifecycle-free
  extension contracts;
- `artifact/{memory,experience,skill,handoff}` for typed Artifact families.

This is a Go source-compatibility break for callers that imported the moved
packages. It is accepted as part of the pre-release package-boundary cleanup.
It does not authorize changes to OpenAPI operations, MCP tools, CLI behavior,
database formats, hashes, prompts, concurrency semantics, or host adapters.

## Consequences

Top-level packages now communicate an intentional support boundary instead of
mirroring implementation categories. Exported identifiers inside product
domains remain useful across the repository without becoming external API
commitments. SQL-native code has one visible owner, while transactional stores
remain cohesive in `internal/sqlstore`.

Tests and frozen Python traceability evidence follow the new paths. A
repository-layout conformance test rejects the former root directories and
generic catch-all directories so this boundary cannot drift silently.
