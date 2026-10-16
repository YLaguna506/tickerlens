from typing import Any

import httpx
import pytest

from tickerlens_agents.llm import LLMError
from tickerlens_agents.llm.openai_compat import OpenAICompatibleClient, error_message, retry_hint

GEMINI_429 = """[{"error": {"code": 429, "message": "You exceeded your current quota.",
  "status": "RESOURCE_EXHAUSTED",
  "details": [{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "25s"}]}}]"""

OK = {
    "model": "m",
    "choices": [{"message": {"content": '{"a": 1}'}}],
    "usage": {"prompt_tokens": 10, "completion_tokens": 5},
}


def test_retry_hint_reads_gemini_retry_delay() -> None:
    assert retry_hint(httpx.Response(429, text=GEMINI_429)) == 26
    assert retry_hint(httpx.Response(503, headers={"Retry-After": "7"})) == 7
    assert retry_hint(httpx.Response(429, text="slow down")) == 20
    assert retry_hint(httpx.Response(503, text="busy")) is None


def test_error_message_strips_the_envelope() -> None:
    assert error_message(GEMINI_429) == "You exceeded your current quota."
    assert error_message("plain text") == "plain text"


def make_client(
    responses: list[httpx.Response], provider: str = "gemini"
) -> tuple[OpenAICompatibleClient, list[Any]]:
    bodies: list[Any] = []

    def handler(request: httpx.Request) -> httpx.Response:
        bodies.append(request.content)
        return responses.pop(0)

    http = httpx.Client(base_url="https://llm.example/v1/", transport=httpx.MockTransport(handler))
    return OpenAICompatibleClient("unused", "key", provider=provider, http=http), bodies


def test_waits_as_long_as_the_provider_asks(monkeypatch: pytest.MonkeyPatch) -> None:
    slept: list[float] = []
    monkeypatch.setattr("time.sleep", slept.append)
    client, _ = make_client([httpx.Response(429, text=GEMINI_429), httpx.Response(200, json=OK)])

    result = client.complete_structured([{"role": "user", "content": "hi"}], {}, "s", "m")

    assert result.text == '{"a": 1}'
    assert slept == [26]


def test_gives_up_with_a_readable_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("time.sleep", lambda _: None)
    client, _ = make_client(
        [httpx.Response(503, text='{"error": {"message": "high demand"}}') for _ in range(4)]
    )

    with pytest.raises(LLMError, match="high demand"):
        client.complete_structured([{"role": "user", "content": "hi"}], {}, "s", "m")


def test_daily_quota_fails_immediately(monkeypatch: pytest.MonkeyPatch) -> None:
    slept: list[float] = []
    monkeypatch.setattr("time.sleep", slept.append)
    daily = (
        '{"error": {"message": "Quota exceeded", "details": '
        '[{"quotaId": "GenerateRequestsPerDayPerProjectPerModel-FreeTier"}]}}'
    )
    client, bodies = make_client([httpx.Response(429, text=daily)])

    with pytest.raises(LLMError, match="daily quota"):
        client.complete_structured([{"role": "user", "content": "hi"}], {}, "s", "m")
    assert slept == [] and len(bodies) == 1


def test_gemini_sends_reasoning_effort() -> None:
    client, bodies = make_client([httpx.Response(200, json=OK)])
    client.complete_structured([{"role": "user", "content": "hi"}], {}, "s", "m")
    assert b'"reasoning_effort":"low"' in bodies[0]
