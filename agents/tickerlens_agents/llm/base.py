"""The LLMClient interface every provider implements."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Protocol

Message = dict[str, str]


class LLMError(Exception):
    """The provider failed or returned something unusable."""


@dataclass
class Usage:
    model: str = ""
    input_tokens: int = 0
    output_tokens: int = 0
    cost_usd: float = 0.0
    cost_known: bool = False

    def add(self, other: Usage) -> None:
        self.model = other.model or self.model
        self.input_tokens += other.input_tokens
        self.output_tokens += other.output_tokens
        self.cost_usd += other.cost_usd
        self.cost_known = self.cost_known or other.cost_known


@dataclass
class LLMResult:
    """Raw text of a structured completion; the caller validates it."""

    text: str
    usage: Usage = field(default_factory=Usage)


class LLMClient(Protocol):
    def complete_structured(
        self, messages: list[Message], schema: dict[str, Any], schema_name: str, model: str
    ) -> LLMResult:
        """Ask for JSON matching `schema` (a JSON Schema) and return the raw text."""
        ...
