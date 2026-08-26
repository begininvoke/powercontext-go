# LoCoMo benchmark

This Go-native runner exercises the real PowerContext chain: timestamped
dialogue Source capture, model-backed Memory extraction, hybrid retrieval,
optional listwise reranking, generated answers, and correctness judging. Its
observable result schemas, dataset, prompts, selection, metrics, fallback
policy, and resume rules are aligned with the frozen Python benchmark.

The code is deliberately outside the production binary:

- `tools/locomo` owns the developer command and process composition.
- `internal/benchmark/locomo` owns deterministic schemas, dataset parsing,
  metrics, prompts, checkpoints, and orchestration.
- `benchmark/locomo/results` is ignored operator output.

## Inspect

Copy `.env.example` to a private environment file and set real database and
provider values. Inspection validates configuration and the frozen dataset
without connecting to either service:

```sh
go run ./tools/locomo inspect --env-file benchmark/locomo/.env.example
```

The canonical shape is 10 conversations, 272 sessions, 5,882 turns, 1,986
questions, and 1,540 scored category 1–4 questions.

## Run and resume

```sh
go run ./tools/locomo run \
  --env-file .env \
  --run-id locomo-smoke \
  --conversation-limit 1 \
  --question-limit 5
```

The default full run uses hybrid Top 30 retrieval. Useful compatible variants
include:

```sh
# Broad retrieval followed by a smaller listwise-selected answer context.
go run ./tools/locomo run --env-file .env --run-id locomo-rerank \
  --top-k 30 --answer-k 10 --rerank-mode llm

# Expand only exact Source sessions cited by selected Memory.
go run ./tools/locomo run --env-file .env --run-id locomo-source \
  --top-k 30 --answer-k 10 --answer-source-content

# Retry only an exact normalized Unknown answer with the inference policy.
go run ./tools/locomo run --env-file .env --run-id locomo-fallback \
  --top-k 30 --answer-k 10 --answer-source-content \
  --answer-unknown-fallback-inference
```

Source IDs and database scopes are stable, Memory flushes advance exactly one
Source, and successful question observations are checkpointed as strict
single-line JSONL. A repeated command resumes ingestion and skips successful
questions; `--keep-errors` retains prior failures as zero-scored observations.
Use `--skip-ingestion` after a namespace is populated or `--skip-evaluation`
to populate it without spending answer/judge requests.

## Independent judge

Freeze generated answers and grade them with a separately configured model:

```sh
go run ./tools/locomo rejudge \
  --env-file .env \
  --source-directory benchmark/locomo/results/locomo-full \
  --output-directory benchmark/locomo/results/locomo-full-rejudge \
  --run-id independent-topical \
  --judge-model openai:your-independent-model
```

The rejudge manifest fingerprints the source observations and answer contract.
A completed run can be reopened without contacting its provider; pending or
retryable errors initialize the judge lazily.

Each run writes `run.json`, `ingestion.json`, `observations.jsonl`,
`summary.json`, and `summary.md`. Result artifacts deliberately exclude
database URLs and credentials.
