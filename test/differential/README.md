# Differential tests

`compare_servers.py` is the executable black-box gate between the frozen
Python `3a6cb0151670eaff7dc0293466edd673124e80da` Server and the Go Server. Each
process receives a private `POWERCONTEXT_HOME` and therefore an independent
SQLite authority database.

The scenario compares health/readiness, capabilities, Source idempotency and
conflicts, verified Work Contracts, current Handoff preparation, commit,
acknowledgement, Task Outcome, known scopes, the scope-centric Handoff Report,
statistics, and missing-evidence errors. It compares public status codes,
JSON, request-ID presence, content type, and cache policy. Only explicit clock,
request-ID, and report-digest fields are normalized.

From the repository root, after installing the frozen Oracle and building the
Go binary:

```bash
make build
_oracle/.venv/bin/python test/differential/compare_servers.py \
  --python-executable _oracle/.venv/bin/powercontext \
  --python-cwd _oracle \
  --go-executable bin/powercontext \
  --go-cwd .
```
