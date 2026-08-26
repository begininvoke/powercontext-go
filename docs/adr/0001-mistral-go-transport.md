# ADR 0001: Use the narrow HTTP adapter for Mistral

- Status: accepted
- Frozen compatibility target: Pydantic AI 2.29.0
- Decision date: 2026-08-17

## Context

The migration plan originally assigned Mistral to an official Go SDK. Mistral's
official SDK catalogue currently lists Python and TypeScript SDKs; its Go entry
is community-maintained. Adding that package would therefore violate the same
plan's requirement not to introduce a large unofficial provider framework.

Sources:

- <https://docs.mistral.ai/resources/sdks>
- <https://github.com/mistralai>

## Decision

Mistral generation uses `internal/modelprovider`'s bounded `net/http` chat
adapter, shared only with providers that lack a stable official Go SDK. The
adapter implements the frozen Pydantic AI wire surface, disables implicit
retries, rejects redirects, caps response bodies, sanitizes provider errors,
and never exposes credentials or response content through errors or telemetry.

Mistral remains a first-class provider in the registry and compatibility
matrix. Fake-transport tests assert its endpoint, bearer authentication,
candidate-count field, structured-output retry behavior, and error mapping.

## Consequences

This is an explicit correction to a factual assumption in the implementation
plan, not a reduction in provider coverage. We can replace the adapter only if
Mistral publishes and supports an official Go SDK whose behavior can pass the
same frozen wire fixtures without changing domain or inference interfaces.
