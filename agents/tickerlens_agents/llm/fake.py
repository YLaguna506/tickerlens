"""A scripted LLMClient for tests: returns queued responses in order."""

from __future__ import annotations

import json
from typing import Any

from tickerlens_agents.llm.base import LLMError, LLMResult, Message, Usage


class FakeClient:
    def __init__(self, responses: list[Any] | None = None) -> None:
        # Each response is a dict (serialized to JSON) or a raw string.
        self.responses = list(responses or [])
        self.calls: list[list[Message]] = []

    def complete_structured(
        self, messages: list[Message], schema: dict[str, Any], schema_name: str, model: str
    ) -> LLMResult:
        self.calls.append(messages)
        if not self.responses:
            raise LLMError("FakeClient: no more scripted responses")
        r = self.responses.pop(0)
        text = r if isinstance(r, str) else json.dumps(r)
        return LLMResult(text=text, usage=Usage(model=model, input_tokens=100, output_tokens=50))
