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

from __future__ import annotations

import json

from powercontext_bub.plugin import _capture_content


def test_capture_content_redacts_sensitive_fields_before_transport(monkeypatch) -> None:
    secret = "provider-secret-sentinel"
    monkeypatch.setenv("BUB_API_KEY", secret)

    content = _capture_content(
        "tool_result",
        1,
        {
            "tool": "provider.request",
            "arguments": {"api_key": secret},
            "result": f"response contained {secret}",
        },
        8_192,
    )

    assert secret not in content
    assert "[REDACTED]" in content
    decoded = json.loads(content)
    assert decoded["event"] == "tool_result"
    assert decoded["payload"]["arguments"]["api_key"] == "[REDACTED]"


def test_capture_content_is_deterministic_and_utf8_byte_bounded(monkeypatch) -> None:
    monkeypatch.delenv("BUB_API_KEY", raising=False)
    payload = {"text": "项目上下文" * 2_000}

    first = _capture_content("user_prompt", 7, payload, 512)
    second = _capture_content("user_prompt", 7, payload, 512)

    assert first == second
    assert len(first.encode("utf-8")) <= 512
    assert json.loads(first)["truncated"] is True
