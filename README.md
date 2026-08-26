# PowerContext Go

PowerContext Go is the Go 1.25 implementation of PowerContext. It preserves
the frozen Python `v0.0.2` observable contract while using Go-native domain
types, lifecycle ownership, concurrency, persistence, transports, and release
packaging.

```text
module github.com/ob-labs/powercontext-go
oracle 3a6cb0151670eaff7dc0293466edd673124e80da
```

The HTTP source of truth is [`openapi/powercontext.yaml`](openapi/powercontext.yaml).
Generated code under `api/v1` and generated operation tables are never edited
by hand. Compatibility evidence lives under `test/conformance`: all 622 frozen
Python test cases are inventoried with resolvable Go or retained-host evidence.

## Repository shape

- `source`, `artifact`, `trigger`, and `inference` are lifecycle-free public
  domain contracts.
- `artifact/{memory,experience,skill,handoff}`, `review`, `contextpack`,
  `handoffreport`, and `stats` implement distinct domain capabilities.
- `runtime` owns admission, Scope boundaries, same-Scope write serialization,
  scheduled processing, and application use cases.
- `client` and `server` are public remote and process facades.
- `internal` contains concrete adapters: SQL, providers, scheduler, endpoints,
  HTTP, MCP, dashboard, CLI, and observability.
- `integrations` contains host-native adapters for Codex, Claude Code, Bub,
  DeepSeek Harness, Hermes, LangGraph, OpenClaw, OpenCode, and Pi. They
  communicate only with the Go Server.
- `test` contains conformance, differential, and process-level suites; `tools`
  contains generators and release tooling.
- `benchmark/locomo` contains operator-facing LoCoMo configuration and result
  space; its Go runner lives in `tools/locomo`, with deterministic internals in
  `internal/benchmark/locomo`.

There is intentionally no `common`, `utils`, generic repository layer, or DI
container. Shared infrastructure exists only where it has one clear owner—for
example, privacy-safe `log/slog` setup under `internal/observability/logging`.

See [`docs/architecture/README.md`](docs/architecture/README.md) for the full
directory map and dependency rules.

## Build and verify

The standard build uses CGO and statically embeds the same sqlite-vec 0.1.9
`vec0` implementation as the Python runtime:

```sh
make check
make build
```

Run the server with the frozen defaults:

```sh
./bin/powercontext server run
```

Server configuration uses `POWERCONTEXT_SERVER_*`; remote CLI configuration
uses `POWERCONTEXT_CLIENT_SERVER_URL`, `POWERCONTEXT_CLIENT_API_TOKEN`, and
`POWERCONTEXT_CLIENT_TIMEOUT`. The full local-embedding build additionally
requires the native tokenizer and ONNX Runtime assets described in
[`docs/release/INSTALL.md`](docs/release/INSTALL.md).

Useful verification targets:

```sh
make check-generated
make test-race
make test-full TOKENIZERS_LIB_DIR=/path/to/tokenizers/lib
POWERCONTEXT_TEST_OCEANBASE_URL='mysql+aoceanbase://root%40tenant:password@127.0.0.1:2881/powercontext?charset=utf8mb4' \
  make test-oceanbase-live
```

The OceanBase target requires a dedicated disposable MySQL-mode database. It
verifies tenant and charset negotiation, the complete core and optional Report
schemas, Source cursor CAS, and Handoff Report Activity allocation against the
real server rather than a SQL mock.

The Go-native LoCoMo benchmark uses the same runtime, database, providers, and
frozen dataset contract as Python:

```sh
go run ./tools/locomo inspect --env-file benchmark/locomo/.env.example
go run ./tools/locomo run --env-file .env --run-id locomo-smoke \
  --conversation-limit 1 --question-limit 5
```

See [`benchmark/locomo/README.md`](benchmark/locomo/README.md) for resumable
ingestion, reranking, Source expansion, and independent rejudging.

Read [`AGENTS.md`](AGENTS.md) before changing package boundaries, persistence
formats, lifecycle ownership, or generated contracts.
