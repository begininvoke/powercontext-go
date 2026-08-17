"""Exercise bidirectional Review/Candidate database compatibility.

The Go conformance test asks the frozen Python runtime to create a pending
Candidate, revises and approves it in place with Go, then asks Python to read
the Go result and append another Candidate.  There is no import/export layer:
both implementations use the same authority database.
"""

from __future__ import annotations

import argparse
import asyncio
from pathlib import Path

from powercontext.builtin.artifacts.experience import ExperienceContent
from powercontext.builtin.persistence.sqlite import SQLiteConfig, SQLiteProfile
from powercontext.builtin.persistence.tables import BUILTIN_TABLES
from powercontext.builtin.runtime.relational import RelationalContexts
from powercontext.builtin.sources import ContentCapture

SCOPE_ID = "project:review-compatibility"
SOURCE_ID = "task-python"
PYTHON_CANDIDATE_ID = "candidate-python"
POST_GO_CANDIDATE_ID = "candidate-python-after-go"


def _config(path: Path) -> SQLiteConfig:
    return SQLiteConfig(url=f"sqlite+aiosqlite:///{path}")


def _proposal(lesson: str) -> ExperienceContent:
    return ExperienceContent(
        situation="The public contract changed.",
        action="Regenerate the client and run contract tests.",
        outcome="The transport stayed aligned.",
        lesson=lesson,
    )


async def _create(path: Path) -> None:
    for candidate in (path, Path(f"{path}-shm"), Path(f"{path}-wal")):
        candidate.unlink(missing_ok=True)
    async with SQLiteProfile.open(_config(path), tables=BUILTIN_TABLES) as profile:
        contexts = RelationalContexts(
            database=profile.database,
            id_factory=lambda kind: PYTHON_CANDIDATE_ID if kind == "candidate" else f"{kind}-python",
        )
        context = await contexts.get(SCOPE_ID)
        source, position = await context.sources.capture(
            ContentCapture(
                source_id=SOURCE_ID,
                content="Python bounded café evidence.",
                metadata={"origin": "python"},
            )
        )
        source_ref = context.sources.catalog.as_ref(source)
        candidate = await contexts.review(SCOPE_ID).propose_experience(
            _proposal("Python initial lesson."),
            sources=(source_ref, source_ref),
            artifacts=(),
            target=None,
            reason="Created by Python café.",
        )
        assert position == 1
        assert candidate.candidate_id == PYTHON_CANDIDATE_ID
        assert candidate.version == 1 and candidate.status == "pending"
        assert candidate.sources == (source_ref,)


async def _verify(path: Path) -> None:
    async with SQLiteProfile.open(_config(path), tables=BUILTIN_TABLES) as profile:
        contexts = RelationalContexts(
            database=profile.database,
            id_factory=lambda kind: POST_GO_CANDIDATE_ID if kind == "candidate" else f"{kind}-python-after-go",
        )
        context = await contexts.get(SCOPE_ID)
        review = contexts.review(SCOPE_ID)
        approved = await review.get_candidate(PYTHON_CANDIDATE_ID)
        assert approved.version == 2 and approved.status == "approved"
        assert approved.reason == "Reviewed by Go café."
        assert approved.proposal.lesson == "Go reviewed lesson."
        assert approved.result_artifact is not None
        assert approved.result_artifact.family == "experience"
        assert approved.result_artifact.artifact_id == "experience-go"
        assert approved.result_artifact.revision == 1

        experience = await review.get_experience(approved.result_artifact)
        assert experience.content == approved.proposal
        assert experience.lineage.sources == approved.sources
        assert experience.lineage.artifacts == ()

        source = (await context.sources.list())[0]
        source_ref = context.sources.catalog.as_ref(source)
        pending = await review.propose_experience(
            _proposal("Python still writes after Go."),
            sources=(source_ref,),
            artifacts=(),
            target=None,
            reason=None,
        )
        assert pending.candidate_id == POST_GO_CANDIDATE_ID
        assert pending.status == "pending" and pending.version == 1


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
