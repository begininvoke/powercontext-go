# OpenAPI

`powercontext.yaml` is copied from the current Python reference baseline and is
the sole source of truth for HTTP wire generation.

OpenAPI 3.0 cannot encode the combined `source_refs` + `artifact_refs` maximum
described by the Candidate schemas. The generator derives the affected model
set from the two `maxItems: 32` declarations and emits the supplemental
`api/v1.ValidatePowerContextContract` validator. HTTP, MCP, and the public Go
Client all invoke it at their decoded transport boundary.
