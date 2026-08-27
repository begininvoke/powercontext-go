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

"""In-process implementation of the three Go Server contracts consumed by the adapter."""

from __future__ import annotations

import json
from collections import defaultdict
from typing import Any

import httpx


class FakePowerContextTransport(httpx.AsyncBaseTransport):
    """Small protocol fake: tests the standalone adapter without importing the Python implementation."""

    def __init__(self) -> None:
        self.entries: dict[str, list[dict[str, Any]]] = defaultdict(list)

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        try:
            payload = json.loads(request.content)
        except (UnicodeDecodeError, json.JSONDecodeError):
            return self._response(
                422,
                {"error": {"code": "invalid_request", "message": "invalid JSON"}},
                request,
            )

        path = request.url.path
        if request.method != "POST":
            return self._response(405, {}, request)
        if path == "/v1/memory/remember":
            return self._remember(request, payload)
        if path == "/v1/memory/search":
            return self._search(request, payload)
        if path == "/v1/context/prepare":
            return self._prepare(request, payload)
        return self._response(404, {}, request)

    def _remember(
        self, request: httpx.Request, payload: dict[str, Any]
    ) -> httpx.Response:
        scope_id = payload["scope_id"]
        revision = len(self.entries[scope_id]) + 1
        memory_ref = {"family": "memory", "artifact_id": "memory", "revision": revision}
        entry = {
            "citation": {
                "memory_ref": memory_ref,
                "entry_id": f"entry-{revision}",
                "entry_version_id": f"entry-{revision}-v1",
            },
            "version": 1,
            "kind": payload["kind"],
            "text": payload["text"],
            "state": "active",
            "source_refs": [],
            "artifact_refs": [],
        }
        self.entries[scope_id].append(entry)
        return self._response(200, {"memory": memory_ref, "entry": entry}, request)

    def _search(
        self, request: httpx.Request, payload: dict[str, Any]
    ) -> httpx.Response:
        entries = self.entries[payload["scope_id"]][: payload.get("limit", 10)]
        hits = [
            {
                "citation": entry["citation"],
                "text": entry["text"],
                "score": 0.75,
                "matched_by": ["fts"],
            }
            for entry in entries
        ]
        memory = hits[0]["citation"]["memory_ref"] if hits else None
        return self._response(
            200,
            {"memory": memory, "mode": "fts" if hits else None, "hits": hits},
            request,
        )

    def _prepare(
        self, request: httpx.Request, payload: dict[str, Any]
    ) -> httpx.Response:
        entries = self.entries[payload["scope_id"]]
        if not entries:
            return self._response(
                200,
                {
                    "schema": "powercontext.prepared-context.v1",
                    "status": "empty",
                    "content": None,
                    "content_bytes": 0,
                },
                request,
            )
        content = "\n".join(entry["text"] for entry in entries)
        content = _truncate_utf8(content, payload.get("max_bytes", 8000))
        return self._response(
            200,
            {
                "schema": "powercontext.prepared-context.v1",
                "status": "ready",
                "content": content,
                "content_bytes": len(content.encode("utf-8")),
            },
            request,
        )

    @staticmethod
    def _response(
        status: int, body: dict[str, Any], request: httpx.Request
    ) -> httpx.Response:
        return httpx.Response(
            status,
            json=body,
            headers={"X-PowerContext-Request-ID": "test-request"},
            request=request,
        )


def _truncate_utf8(value: str, limit: int) -> str:
    encoded = value.encode("utf-8")
    if len(encoded) <= limit:
        return value
    return encoded[:limit].decode("utf-8", errors="ignore")
