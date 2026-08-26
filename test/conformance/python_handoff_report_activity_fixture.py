"""Exercise bidirectional Handoff Report Activity database compatibility.

The Go conformance test invokes ``create`` before opening the database and
``verify`` after extending it.  This script deliberately uses only the frozen
Python repository boundary, so a passing test proves that neither side relies
on a private import/export translation.
"""

from __future__ import annotations

import argparse
import asyncio
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path

from powercontext.builtin.handoff_report.models import ReportActivityEvent
from powercontext.builtin.handoff_report.sqlite import HANDOFF_REPORT_TABLES, SQLiteActivityEventRepository
from powercontext.builtin.persistence.sqlite import SQLiteConfig, SQLiteProfile


def _config(path: Path) -> SQLiteConfig:
    return SQLiteConfig(url=f"sqlite+aiosqlite:///{path}")


def _python_event() -> ReportActivityEvent:
    return ReportActivityEvent(
        event_id="event-python",
        project_id="project-1",
        source="git_commit",
        source_event_id="git:python-stable",
        occurred_at=datetime(2026, 8, 5, 9, 0, 0, 123000, tzinfo=UTC),
        observed_at=datetime(2026, 8, 5, 10, 0, 0, 456000, tzinfo=UTC),
        time_basis="source_reported",
        title="Python café <capture>",
    )


async def _create(path: Path) -> None:
    repository = SQLiteActivityEventRepository()
    async with SQLiteProfile.open(_config(path), tables=HANDOFF_REPORT_TABLES) as profile:
        async with profile.database.transaction() as connection:
            stored = await repository.record(connection, _python_event())
            assert stored.cursor == 1
            assert stored.event_id == "event-python"
            assert await repository.high_watermark(connection, "project-1") == 1


async def _verify(path: Path) -> None:
    repository = SQLiteActivityEventRepository()
    async with SQLiteProfile.open(_config(path), tables=HANDOFF_REPORT_TABLES) as profile:
        async with profile.database.transaction() as connection:
            stored = await repository.list(connection, "project-1")
            assert [item.cursor for item in stored] == [1, 2]
            assert [item.event_id for item in stored] == ["event-python", "event-go"]

            python_event = ReportActivityEvent.model_validate_json(json.dumps(stored[0].payload))
            go_event = ReportActivityEvent.model_validate_json(json.dumps(stored[1].payload))
            assert python_event.title == "Python café <capture>"
            assert go_event.title == "Go café <capture>"
            assert go_event.observed_at == datetime(2026, 8, 5, 12, 0, 0, 654321, tzinfo=UTC)
            assert go_event.occurred_at is None

            retry = go_event.model_copy(
                update={
                    "event_id": "event-python-retry-for-go",
                    "observed_at": go_event.observed_at + timedelta(minutes=5),
                }
            )
            repeated = await repository.record(connection, retry)
            assert repeated.cursor == 2
            assert repeated.event_id == "event-go"
            assert repeated.observed_at == go_event.observed_at
            assert await repository.high_watermark(connection, "project-1") == 2


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("create", "verify"))
    parser.add_argument("database", type=Path)
    arguments = parser.parse_args()
    if arguments.mode == "create":
        asyncio.run(_create(arguments.database))
    else:
        asyncio.run(_verify(arguments.database))


if __name__ == "__main__":
    main()
