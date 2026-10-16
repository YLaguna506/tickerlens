"""Build the configured LLM client from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass

from tickerlens_agents.llm.base import LLMClient, LLMError
from tickerlens_agents.llm.openai_compat import OpenAICompatibleClient

BASE_URLS = {
    "openai": "https://api.openai.com/v1",
    "openrouter": "https://openrouter.ai/api/v1",
}
KEY_VARS = {"openai": "OPENAI_API_KEY", "openrouter": "OPENROUTER_API_KEY"}


@dataclass
class Models:
    fast: str
    strong: str


def models_from_env() -> Models:
    fast = os.getenv("LLM_MODEL_FAST", "")
    return Models(fast=fast, strong=os.getenv("LLM_MODEL_STRONG", "") or fast)


def client_from_env() -> LLMClient:
    """LLM_PROVIDER picks openai or openrouter (both OpenAI-compatible).
    LLM_BASE_URL overrides the endpoint, e.g. for a local server."""
    provider = os.getenv("LLM_PROVIDER", "openrouter").lower()
    if provider not in BASE_URLS:
        raise LLMError(f"unknown LLM_PROVIDER {provider!r}; use openai or openrouter")
    base_url = os.getenv("LLM_BASE_URL") or BASE_URLS[provider]
    return OpenAICompatibleClient(
        base_url,
        os.getenv(KEY_VARS[provider], ""),
        provider=provider,
        reasoning_effort=os.getenv("LLM_REASONING_EFFORT", "low"),
    )
