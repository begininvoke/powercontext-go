# Copyright (c) 2026 OceanBase.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Generate and compare the frozen APScheduler 3.11.3 sidecar."""

from __future__ import annotations

import argparse
import asyncio
import base64
import difflib
import json
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
TABLE_NAME = "powercontext_scheduler_jobs"
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
            trigger=IntervalTrigger(
                seconds=3_600, start_date=SOURCE_START, timezone=UTC
            ),
        )
        scheduler.modify_job(
            SOURCE_WINDOW_JOB_ID,
            args=(CANONICAL_PATH,),
            next_run_time=SOURCE_START,
        )
        scheduler.reschedule_job(
            EXPERIENCE_INCUBATION_JOB_ID,
            trigger=IntervalTrigger(
                seconds=7_200, start_date=EXPERIENCE_START, timezone=UTC
            ),
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
        assert [job.id for job in jobs] == [
            EXPERIENCE_INCUBATION_JOB_ID,
            SOURCE_WINDOW_JOB_ID,
        ]
        assert [job.args for job in jobs] == [(runtime_path,), (runtime_path,)]
        assert jobs[0].trigger.interval.total_seconds() == 7_200
        assert jobs[1].trigger.interval.total_seconds() == 3_600
        assert jobs[0].next_run_time == EXPERIENCE_START
        assert jobs[1].next_run_time == SOURCE_START
        assert all(
            job.coalesce and job.max_instances == 1 and job.misfire_grace_time is None
            for job in jobs
        )
    finally:
        store.shutdown()


def semantic_snapshot(path: Path) -> dict[str, object]:
    """Return the portable scheduler contract, excluding SQLite container metadata."""
    if not path.is_file():
        raise FileNotFoundError(f"scheduler database does not exist: {path}")
    database_uri = f"{path.resolve().as_uri()}?mode=ro"
    with sqlite3.connect(database_uri, uri=True) as connection:
        schema = [
            {"type": row[0], "name": row[1], "table": row[2], "sql": row[3]}
            for row in connection.execute(
                "SELECT type, name, tbl_name, sql FROM sqlite_master "
                "WHERE name = ? OR tbl_name = ? ORDER BY type, name",
                (TABLE_NAME, TABLE_NAME),
            )
        ]
        rows = [
            {
                "id": row[0],
                "next_run_time": row[1],
                "job_state": base64.b64encode(row[2]).decode("ascii"),
            }
            for row in connection.execute(
                f'SELECT id, next_run_time, job_state FROM "{TABLE_NAME}" ORDER BY id'
            )
        ]
    return {"schema": schema, "rows": rows}


def compare_databases(actual: Path, expected: Path) -> None:
    actual_json = json.dumps(semantic_snapshot(actual), indent=2, sort_keys=True)
    expected_json = json.dumps(semantic_snapshot(expected), indent=2, sort_keys=True)
    if actual_json == expected_json:
        return
    difference = "\n".join(
        difflib.unified_diff(
            expected_json.splitlines(),
            actual_json.splitlines(),
            fromfile=str(expected),
            tofile=str(actual),
            lineterm="",
        )
    )
    raise AssertionError(f"scheduler database contract differs:\n{difference}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("generate", "verify", "compare"))
    parser.add_argument("database", type=Path)
    parser.add_argument("--runtime-path", default=CANONICAL_PATH)
    parser.add_argument("--expected", type=Path)
    arguments = parser.parse_args()
    if arguments.mode == "generate":
        asyncio.run(generate(arguments.database))
    elif arguments.mode == "verify":
        verify(arguments.database, arguments.runtime_path)
    else:
        if arguments.expected is None:
            parser.error("compare requires --expected")
        compare_databases(arguments.database, arguments.expected)


if __name__ == "__main__":
    main()
