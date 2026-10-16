"""Client for OpenAI-compatible chat completion APIs (OpenAI, OpenRouter).

Plain HTTP (httpx) instead of a provider SDK keeps the request explicit and
works the same against any OpenAI-compatible endpoint.
"""

from __future__ import annotations

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
        max_attempts: int = 3,
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
        delay = 2.0
        last = ""
        for attempt in range(self.max_attempts):
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
                elif resp.status_code in RETRY_STATUSES:
                    last = f"HTTP {resp.status_code}: {resp.text[:300]}"
                    retry_after = resp.headers.get("Retry-After", "")
                    if retry_after.isdigit():
                        delay = min(float(retry_after), 30)
                else:
                    raise LLMError(f"{self.provider}: HTTP {resp.status_code}: {resp.text[:300]}")
            if attempt + 1 < self.max_attempts:
                time.sleep(delay)
                delay *= 2
        raise LLMError(f"{self.provider}: giving up after {self.max_attempts} attempts: {last}")


def strip_fences(text: str) -> str:
    """Some models wrap JSON in ```json fences despite being told not to."""
    t = text.strip()
    if t.startswith("```"):
        t = t.split("\n", 1)[1] if "\n" in t else ""
        if t.rstrip().endswith("```"):
            t = t.rstrip()[:-3]
    return t.strip()
