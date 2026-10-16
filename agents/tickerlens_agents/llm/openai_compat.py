"""Client for OpenAI-compatible chat completion APIs (OpenAI, OpenRouter).

Plain HTTP (httpx) instead of a provider SDK keeps the request explicit and
works the same against any OpenAI-compatible endpoint.
"""

from __future__ import annotations

import re
import time
from typing import Any

import httpx

from tickerlens_agents.llm.base import LLMError, LLMResult, Message, Usage

RETRY_STATUSES = {429, 500, 502, 503, 504}


class OpenAICompatibleClient:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        provider: str,
        temperature: float = 0.1,
        # Reasoning models spend output tokens thinking before they answer;
        # too small a budget leaves an empty reply.
        max_tokens: int = 12000,
        reasoning_effort: str = "low",
        timeout_s: float = 90,
        max_attempts: int = 4,
        http: httpx.Client | None = None,
    ) -> None:
        if not api_key:
            raise LLMError(f"no API key configured for {provider}")
        self.provider = provider
        self.temperature = temperature
        self.max_tokens = max_tokens
        self.reasoning_effort = reasoning_effort
        self.max_attempts = max_attempts
        headers = {"Authorization": f"Bearer {api_key}"}
        if provider == "openrouter":
            headers["X-Title"] = "TickerLens"
        self.http = http or httpx.Client(base_url=base_url, headers=headers, timeout=timeout_s)

    def complete_structured(
        self, messages: list[Message], schema: dict[str, Any], schema_name: str, model: str
    ) -> LLMResult:
        body: dict[str, Any] = {
            "model": model,
            "messages": messages,
            "temperature": self.temperature,
            "max_tokens": self.max_tokens,
            "response_format": {
                "type": "json_schema",
                "json_schema": {"name": schema_name, "strict": True, "schema": schema},
            },
        }
        if self.provider == "openrouter":
            body["usage"] = {"include": True}  # ask OpenRouter to report the cost
            # Unified reasoning control; models without reasoning ignore it.
            if self.reasoning_effort:
                body["reasoning"] = {"effort": self.reasoning_effort}
        elif self.provider == "gemini" and self.reasoning_effort:
            # Gemini's OpenAI-compatible API maps this to its thinking budget.
            body["reasoning_effort"] = self.reasoning_effort

        data = self._post(body)
        try:
            text = data["choices"][0]["message"]["content"] or ""
        except (KeyError, IndexError, TypeError) as e:
            raise LLMError(f"{self.provider}: unexpected response shape: {data!r:.300}") from e

        u = data.get("usage") or {}
        cost = u.get("cost")
        usage = Usage(
            model=data.get("model", model),
            input_tokens=int(u.get("prompt_tokens") or 0),
            output_tokens=int(u.get("completion_tokens") or 0),
            cost_usd=float(cost or 0),
            cost_known=cost is not None,
        )
        return LLMResult(text=strip_fences(text), usage=usage)

    def _post(self, body: dict[str, Any]) -> dict[str, Any]:
        backoff = 2.0
        last = ""
        for attempt in range(self.max_attempts):
            wait = backoff
            try:
                resp = self.http.post("/chat/completions", json=body)
            except httpx.HTTPError as e:
                last = f"network error: {e}"
            else:
                if resp.status_code == 200:
                    data: dict[str, Any] = resp.json()
                    if "error" in data:  # OpenRouter can report upstream errors in a 200
                        last = f"provider error: {data['error']}"
                    else:
                        return data
                elif resp.status_code == 429 and "PerDay" in resp.text:
                    # A daily quota will not reset by waiting: fail now so the
                    # caller can switch to its fallback model.
                    detail = error_message(resp.text)[:120]
                    raise LLMError(f"{self.provider}: HTTP 429: daily quota used up ({detail})")
                elif resp.status_code in RETRY_STATUSES:
                    last = f"HTTP {resp.status_code}: {error_message(resp.text)}"
                    if hint := retry_hint(resp):
                        wait = hint
                else:
                    raise LLMError(
                        f"{self.provider}: HTTP {resp.status_code}: {error_message(resp.text)}"
                    )
            if attempt + 1 < self.max_attempts:
                time.sleep(wait)
                backoff *= 2
        raise LLMError(f"{self.provider}: giving up after {self.max_attempts} attempts: {last}")


def retry_hint(resp: httpx.Response, cap: float = 60) -> float | None:
    """Seconds the provider asked us to wait: the Retry-After header, or
    Gemini's RetryInfo ("retryDelay": "25s") inside the error body."""
    header = resp.headers.get("Retry-After", "")
    if header.isdigit():
        return min(float(header), cap)
    m = re.search(r'"retryDelay"\s*:\s*"(\d+(?:\.\d+)?)s"', resp.text)
    if m:
        return min(float(m.group(1)) + 1, cap)
    if resp.status_code == 429:
        return 20.0  # per-minute quotas reset within a minute
    return None


def error_message(text: str) -> str:
    """The provider's error message, without the full JSON envelope."""
    m = re.search(r'"message"\s*:\s*"((?:[^"\\]|\\.)*)"', text)
    return (m.group(1) if m else text)[:300]


def strip_fences(text: str) -> str:
    """Some models wrap JSON in ```json fences despite being told not to."""
    t = text.strip()
    if t.startswith("```"):
        t = t.split("\n", 1)[1] if "\n" in t else ""
        if t.rstrip().endswith("```"):
            t = t.rstrip()[:-3]
    return t.strip()
