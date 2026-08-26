# Python v0.0.1 reference fixtures

These files were produced by the Python repository at exactly
`9e23c336492c8bba16c6f26083298b6f484a91b0`:

- `authority.db` and `authority-rows.json`: authoritative `pc_*` SQLite data,
  complete schema objects, and a semantic row snapshot;
- `scheduler.db`: the two APScheduler 3.11.3 protocol-5 jobs;
- `domain-contract.json`: 80 exported constants, 43 HTTP error mappings, and
  byte-exact Memory entry/revision/embedding canonicalization samples;
- `provider-matrix.json`: Pydantic AI 2.29.0 generation, embedding, and Gateway
  routing prefixes;
- `handoff-report-digests.json`: canonical selection envelope, JSON report,
  RFC 8785 selection digest, and report digest;
- `manifest.json`: hashes for every fixture plus the 87-file/413-case Python
  test inventory.

Fixtures are immutable compatibility evidence, not seed data. CI regenerates
them in a temporary directory and compares bytes before running Python → Go →
Python round trips. The Go tests never mutate the committed databases.
