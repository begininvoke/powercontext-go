# Python compatibility fixture generator

`go run ./tools/fixture-generate -python ../powercontext` freezes the observable
Python oracle used by the Go port. The command refuses to run against a
different commit and writes only deterministic metadata under
`test/conformance/testdata/python-v0.0.2`.

The generated manifest records the authoritative OpenAPI digest, prompt source
digests, exact rendered prompt digests (including Memory reranking), and every
discovered Python test. The rendered values are extracted with a restricted
parser rather than by importing a mutable Python environment. Rich
request/response and database fixtures are stored beside this manifest.

Use `-check` in CI and before review. Test-to-implementation coverage is a
separate generated contract owned by `tools/traceability-generate`.
