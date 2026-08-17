"""Generate the frozen APScheduler 3.11.3 sidecar through its real JobStore."""

from __future__ import annotations

import argparse
import asyncio
import sqlite3
from datetime import UTC, datetime
from pathlib import Path

from apscheduler.triggers.interval import IntervalTrigger

from powercontext.builtin.runtime.scheduler import (
    EXPERIENCE_INCUBATION_JOB_ID,
    SOURCE_WINDOW_JOB_ID,
    configure_experience_incubation_job,
    configure_source_window_job,
    create_scheduler,
)

CANONICAL_PATH = "/powercontext-fixtures/scheduler.db"
SOURCE_START = datetime(2026, 8, 17, 1, 2, 3, 456789, tzinfo=UTC)
EXPERIENCE_START = datetime(2026, 8, 17, 2, 3, 4, 567890, tzinfo=UTC)


async def generate(path: Path) -> None:
    for candidate in (path, Path(f"{path}-shm"), Path(f"{path}-wal")):
        candidate.unlink(missing_ok=True)
    scheduler = create_scheduler(path)
    scheduler.start(paused=True)
    try:
        configure_source_window_job(
            scheduler,
            runtime_key=CANONICAL_PATH,
            schedule_seconds=3_600,
        )
        configure_experience_incubation_job(
            scheduler,
            runtime_key=CANONICAL_PATH,
            schedule_seconds=7_200,
        )
        scheduler.reschedule_job(
            SOURCE_WINDOW_JOB_ID,
            trigger=IntervalTrigger(seconds=3_600, start_date=SOURCE_START, timezone=UTC),
        )
        scheduler.modify_job(
            SOURCE_WINDOW_JOB_ID,
            args=(CANONICAL_PATH,),
            next_run_time=SOURCE_START,
        )
        scheduler.reschedule_job(
            EXPERIENCE_INCUBATION_JOB_ID,
            trigger=IntervalTrigger(seconds=7_200, start_date=EXPERIENCE_START, timezone=UTC),
        )
        scheduler.modify_job(
            EXPERIENCE_INCUBATION_JOB_ID,
            args=(CANONICAL_PATH,),
            next_run_time=EXPERIENCE_START,
        )
    finally:
        scheduler.shutdown(wait=True)
        await asyncio.sleep(0)
    with sqlite3.connect(path) as connection:
        connection.execute("PRAGMA journal_mode=DELETE")
        connection.execute("VACUUM")


def verify(path: Path, runtime_path: str) -> None:
    from apscheduler.jobstores.sqlalchemy import SQLAlchemyJobStore
    from sqlalchemy import URL

    store = SQLAlchemyJobStore(
        url=URL.create("sqlite+pysqlite", database=str(path)),
        tablename="powercontext_scheduler_jobs",
    )
    store.start(None, "default")
    try:
        jobs = sorted(store.get_all_jobs(), key=lambda job: job.id)
        assert [job.id for job in jobs] == [EXPERIENCE_INCUBATION_JOB_ID, SOURCE_WINDOW_JOB_ID]
        assert [job.args for job in jobs] == [(runtime_path,), (runtime_path,)]
        assert jobs[0].trigger.interval.total_seconds() == 7_200
        assert jobs[1].trigger.interval.total_seconds() == 3_600
        assert jobs[0].next_run_time == EXPERIENCE_START
        assert jobs[1].next_run_time == SOURCE_START
        assert all(job.coalesce and job.max_instances == 1 and job.misfire_grace_time is None for job in jobs)
    finally:
        store.shutdown()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("generate", "verify"))
    parser.add_argument("database", type=Path)
    parser.add_argument("--runtime-path", default=CANONICAL_PATH)
    arguments = parser.parse_args()
    if arguments.mode == "generate":
        asyncio.run(generate(arguments.database))
    else:
        verify(arguments.database, arguments.runtime_path)


if __name__ == "__main__":
    main()
