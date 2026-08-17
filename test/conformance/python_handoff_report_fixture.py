"""Generate the frozen Python Handoff Report canonical-digest fixture.

The scenario intentionally uses only the public frozen Python API.  The Go
conformance test constructs the same domain values independently and compares
both RFC 8785 digests, so neither implementation can silently redefine the
wire projection or its canonicalization rules.
"""

from __future__ import annotations

import argparse
import asyncio
import json
from datetime import UTC, datetime
from pathlib import Path

from powercontext.artifacts import ArtifactRef
from powercontext.builtin.artifacts.handoff import (
    Handoff,
    HandoffContent,
    HandoffSourceCitation,
    HandoffStatement,
)
from powercontext.builtin.handoff_report.errors import HandoffReportEvidenceCheckUnavailableError
from powercontext.builtin.handoff_report.canonical import canonical_json_bytes, selection_envelope
from powercontext.builtin.handoff_report.models import (
    ProjectDescriptor,
    ReportActivityEvent,
    WorkstreamDescriptor,
)
from powercontext.builtin.handoff_report.service import HandoffReportService
from powercontext.sources import SourceRef

ORACLE_COMMIT = "9e23c336492c8bba16c6f26083298b6f484a91b0"


def handoff() -> Handoff:
    source = SourceRef(source_type="content", source_id="source-1")
    citation = HandoffSourceCitation(source_ref=source)
    return Handoff(
        artifact_id="handoff",
        revision=1,
        content=HandoffContent(
            objective="Implement the report.",
            state=(HandoffStatement(text="The model exists.", citations=(citation,)),),
            disposition="continuable",
            next_action=HandoffStatement(text="Add the API.", citations=(citation,)),
        ),
        direct_sources=(source,),
    )


class Adapter:
    def __init__(self) -> None:
        self.values: dict[str, Handoff | None] = {"scope-a": handoff(), "scope-b": None}

    async def latest(self, scope_id: str, /) -> Handoff | None:
        return self.values[scope_id]

    async def get(self, scope_id: str, reference: ArtifactRef, /) -> Handoff:
        value = self.values[scope_id]
        assert value is not None and value.as_ref() == reference
        return value

    async def revisions(self, scope_id: str, /) -> tuple[Handoff, ...]:
        value = self.values[scope_id]
        return () if value is None else (value,)

    async def check_evidence(self, scope_id: str, reference: ArtifactRef, /):
        del scope_id, reference
        raise HandoffReportEvidenceCheckUnavailableError


async def generate() -> dict[str, object]:
    project = ProjectDescriptor(
        project_id="prj-1",
        project_key="powercontext",
        title="PowerContext <script>",
        timezone="Asia/Shanghai",
        version=1,
    )
    workstreams = (
        WorkstreamDescriptor(
            scope_id="scope-b",
            project_id="prj-1",
            title="Report",
            kind="feature",
            version=1,
        ),
        WorkstreamDescriptor(
            scope_id="scope-a",
            project_id="prj-1",
            title="Report",
            kind="feature",
            version=1,
        ),
    )
    activity = ReportActivityEvent(
        event_id="evt-a",
        project_id="prj-1",
        scope_id="scope-a",
        source="coding_session",
        source_event_id="source-evt-a",
        occurred_at=datetime(2026, 8, 5, 1, tzinfo=UTC),
        observed_at=datetime(2026, 8, 5, 2, tzinfo=UTC),
        time_basis="source_reported",
    )
    report = await HandoffReportService(Adapter()).generate(
        project,
        workstreams,
        include_evidence_checks=True,
        activities=(activity,),
        activity_cursor=7,
        activity_coverage="captured",
        generated_at=datetime(2026, 8, 6, tzinfo=UTC),
        report_format="json",
        report_kind="handoff",
        normalized_filters={},
    )
    assert report.selection_digest is not None
    assert report.report_digest is not None
    return {
        "schema": "powercontext.python-v0.0.1.handoff-report-digests.v1",
        "oracle_commit": ORACLE_COMMIT,
        "selection_digest": report.selection_digest,
        "report_digest": report.report_digest,
        "selection_envelope": json.loads(canonical_json_bytes(selection_envelope(report))),
        "report": report.model_dump(mode="json", by_alias=True),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    arguments = parser.parse_args()
    fixture = asyncio.run(generate())
    arguments.output.write_text(
        json.dumps(fixture, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
