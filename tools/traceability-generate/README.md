# Python test traceability generator

`go run ./tools/traceability-generate` expands the rules in
`test/conformance/traceability-rules.json` into a deterministic entry for each
frozen Python test function. Every `go:`, `py:`, or `ts:` evidence reference
must resolve to a real repository test declaration.

The rules distinguish evidence deliberately assigned to one case from tests
that only support a Python test file as a group:

- `supporting_evidence` produces `file-supporting` entries and is not a parity
  claim.
- `cases` produces explicit `case-specific` entries.
- `case_evidence` is reserved for one-to-one templates containing
  `{python_test}`, such as retained host-adapter tests with identical names.

`case_specific_evidence_minimum` is a non-regression checkpoint and must equal
the generated case-specific count. Raise it whenever new Python cases gain
explicit evidence; do not lower it to make generation pass.

Use `go run ./tools/traceability-generate -check` to reject missing source
tests, stale output, unknown rules, path traversal, and nonexistent evidence.
The generated `traceability.json` is checked by `go test` as well as CI.
