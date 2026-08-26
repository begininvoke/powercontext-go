# Architecture

PowerContext Go is a contract-first modular monolith. It ports Python behavior,
not Python module layout: packages follow stable domain and ownership
boundaries, while files inside a package are grouped by one responsibility.

## Final directory structure

```text
powercontext-go/
├── api/v1/                    generated OpenAPI wire types and contracts
├── artifact/                  immutable Artifact core and typed families
│   ├── experience/            experience generation, prompts, validation
│   ├── handoff/               handoff content, evidence, resolution, service
│   ├── memory/                authority model, extraction, search, indexing
│   └── skill/                 managed and external Skill behavior
├── benchmark/locomo/          operator config, documentation, ignored results
├── build/                     native release-asset manifest
├── client/                    typed client for all OpenAPI operations
├── cmd/powercontext/          single executable entrypoint
├── contextpack/               bounded context assembly
├── docs/                      architecture, ADRs, RFCs, release guidance
├── handoffreport/             catalog, activity, selection, digest, rendering
├── inference/                 provider-neutral generation and embeddings
├── integrations/
│   ├── bub/                   retained Python host adapter
│   ├── codex/                 retained Python Codex plugin
│   └── dsh/                   retained TypeScript DSH plugin
├── internal/
│   ├── benchmark/             bounded benchmark adapters and fixtures
│   ├── cli/                   Cobra command implementation
│   ├── endpoint/              one application-operation boundary for HTTP/MCP
│   ├── httpapi/               OpenAPI HTTP transport and middleware
│   ├── jcs/                   RFC 8785 canonical JSON boundary
│   ├── mcpapi/                fixed 16 + optional 2 MCP tool surface
│   ├── modelprovider/         concrete remote/local provider adapters
│   ├── observability/         privacy-safe logging, metrics, and tracing
│   ├── scheduler/             interval scheduler and bounded APScheduler Pickle
│   ├── sqlstore/              SQLite/OceanBase stores, codecs, projections
│   ├── testkit/               internal deterministic test doubles
│   └── webui/                 embedded Dashboard templates and assets
├── openapi/                   authoritative HTTP contract and generation hook
├── review/                    Candidate generation/revision/approval domain
├── runtime/                   lifecycle, Scope gates, application orchestration
├── server/                    configuration and process composition root
├── source/                    Source adapters, values, catalog, journal
├── stats/                     statistics domain assembly
├── test/
│   ├── conformance/           frozen Python Oracle and compatibility evidence
│   ├── differential/          black-box Python/Go comparisons
│   └── e2e/                   process and backend vertical slices
├── tools/                     contract, fixture, smoke, benchmark, release tools
└── trigger/                   lifecycle-free trigger values
```

Top-level packages are public only when users or integrations need their types.
Concrete technology choices stay under `internal`; process startup stays in
`server` and `cmd`. This avoids both a Python-shaped `src` tree and a large
catch-all infrastructure package.

## Dependency direction

```text
powercontext
  → source / artifact / trigger / inference
  → artifact families / review / contextpack / stats / handoffreport
  → runtime
  → internal/endpoint
  → internal/httpapi / internal/mcpapi / internal/webui
  → server / cmd
```

`client` depends on generated wire contracts, not domain internals. Host
integrations depend on the published HTTP contract and never import Go domain
or persistence code. `internal/sqlstore` may depend on domain packages but not
on runtime, server, or transports.

## Ownership and lifecycle

- `powercontext.PowerContext` is a lifecycle-free typed composition value.
- Domain values validate at construction and copy mutable slices/maps at their
  boundaries.
- `runtime.Runtime` admits operations, rejects new work during shutdown, drains
  active work, serializes exact-Scope writes, and leaves reads concurrent.
- `server.Application` is the process composition root. It opens concrete
  resources, builds use cases, exposes the shared endpoint, and closes owned
  resources in order.
- `internal/endpoint` is the only application-operation boundary. HTTP and MCP
  adapt into it directly; MCP never loops back through HTTP.

## Authority and transactions

Immutable Artifact revisions, Memory manifests/entry versions, and Source
journal state are authoritative. FTS and vector indexes are projections that
can be rebuilt from the authority state.

Inference and filesystem/provider calls occur outside SQL transactions. A
transaction is opened only for the final authority update: Candidate/Artifact
CAS, associated cursor CAS, and projection updates commit together. Stores
accept the narrow `DBTX` surface required by the use case; there is no generic
repository abstraction.

SQLite retains the Python `pc_*` schema and APScheduler sidecar format.
OceanBase uses explicit capability probing and backend-specific FTS/vector
implementations while preserving the same domain behavior.

## File organization

Large packages use cohesive same-package files instead of subpackages created
only to shorten files. Examples:

- Memory separates write orchestration, search/read behavior, and immutable
  value helpers.
- Handoff separates content/evidence from prepared-resolution semantics.
- Handoff Report separates catalog models, activity events, validation, JSON
  projection, selection, and rendering.
- Scheduler separates the allowlisted Pickle job model, bounded reader, and
  protocol-5 writer.
- Server separates process composition, provider dependency assembly, and
  process support/observability.

This keeps unexported invariants shared without adding import cycles or
artificial `common`, `models`, `services`, or `repositories` packages.

## Contract and compatibility controls

- `openapi/powercontext.yaml` is the HTTP truth; `api/v1` is generated.
- Prompt files are embedded from each family's `prompts` directory and checked
  by frozen SHA-256 fixtures.
- `test/conformance` freezes the Python commit, schemas, prompts, provider
  matrix, SQLite/Vec1/scheduler fixtures, and exact digest behavior.
- `tools/process-smoke` executes the built binary through CLI, HTTP, auth,
  Dashboard, MCP 16+2, restart persistence, and graceful shutdown.
- `tools/locomo` runs or resumes the real LoCoMo pipeline while benchmark
  schemas, metrics, prompts, and the frozen dataset remain under
  `internal/benchmark/locomo`.
- Generated-contract checks fail on OpenAPI, MCP schema, client invocation, DSH
  operation, or traceability drift.
- Compatibility changes to persistence, lifecycle, package direction, or host
  boundaries require an ADR.
