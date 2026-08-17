# Conformance tests

This directory makes the Python `v0.0.1` implementation at commit
`9e23c336492c8bba16c6f26083298b6f484a91b0` an executable Oracle rather than
an informal reference.

- `testdata/python-v0.0.1/manifest.json` freezes OpenAPI, SQLite schema,
  Prompt, fixture, and Python-test inventories.
- `traceability.json` inventories every one of the 413 frozen Python test
  functions and labels its evidence level. `case-specific` means the case has
  an explicit mapping (or a retained-host one-to-one template);
  `file-supporting` means the referenced tests exercise the same component but
  are not yet a claim of scenario equivalence. It is generated from
  `traceability-rules.json`; do not edit the generated table by hand.
- `authority_test.go` proves Python SQLite → Go read/write → Python back-read.
- `review_database_test.go` proves Python Candidate → Go revise/approve →
  Python Artifact back-read and continued Candidate writes against the same
  SQLite authority database.
- the scheduler suite proves APScheduler → Go rewrite → APScheduler restore.
- the domain and Handoff Report fixtures compare exact constants, errors,
  Memory canonical bytes/hashes (including frozen Python whitespace),
  canonical JSON, and digests.

Regenerate or check the two inventories with:

```sh
go run ./tools/fixture-generate -python ../powercontext
go run ./tools/fixture-generate -python ../powercontext -check
go run ./tools/traceability-generate
go run ./tools/traceability-generate -check
```

The frozen-Oracle CI job independently checks out the exact Python commit,
runs its suite, regenerates the deterministic fixtures, and enables both
Python back-read tests. A changed Oracle commit requires deliberate fixture
regeneration and review; changing only a hash to silence drift is not valid.

The rules also carry a monotonically increasing checkpoint for case-specific
evidence. A new exact port must add a `cases` mapping and raise that checkpoint
in the same change.
Shared file-level evidence remains visible as migration debt instead of being
counted as 1:1 parity.
