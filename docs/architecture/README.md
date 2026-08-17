# Architecture

PowerContext Go is a contract-first modular monolith.

The public packages expose the Core SDK, built-in Artifact families, runtime,
server, and client. Concrete SQL, provider, scheduler, transport, CLI, and
observability implementations remain under `internal`.

The implementation preserves observable behavior instead of mirroring Python
files. In particular:

- Source, Artifact, and Trigger remain lifecycle-free Core concepts.
- Artifact revisions and lineage are immutable and exact.
- inference is performed outside SQL transactions;
- Artifact or Candidate writes and cursor CAS commit atomically;
- read paths remain concurrent while same-scope mutations serialize;
- OpenAPI wire models remain distinct from domain models;
- host integrations remain in the language required by their host ABI.

Detailed compatibility and persistence decisions should be recorded as ADRs
before their implementation lands.
