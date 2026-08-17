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
