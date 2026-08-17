# PowerContext Go

An idiomatic Go implementation of PowerContext, preserving the original
project's public contracts, domain semantics, persistence behavior, and
runtime guarantees.

The module path is:

```text
github.com/thunguo/powercontext-go
```

The repository is currently an architecture scaffold. The authoritative HTTP
contract lives in `openapi/powercontext.yaml`; generated wire code belongs in
`api/v1` and must not contain domain behavior.

## Package map

- `source`, `artifact`, and `trigger` contain the public Core SDK.
- `artifact/*` contains the built-in Artifact families.
- `runtime` owns scope, lifecycle, concurrency, and use-case orchestration.
- `client` and `server` are the public remote-access facades.
- `internal` contains SQL, inference-provider, scheduler, transport, CLI, and
  observability implementations.
- `integrations` contains host-native Codex, DeepSeek Harness, and Bub adapters.

See `docs/architecture/README.md` and `AGENTS.md` before adding implementation.
