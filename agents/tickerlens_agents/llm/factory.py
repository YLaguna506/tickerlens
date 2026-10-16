"""Build the configured LLM client from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass

from tickerlens_agents.llm.base import LLMClient, LLMError
from tickerlens_agents.llm.openai_compat import OpenAICompatibleClient

# Every provider speaks the OpenAI chat-completions API.
BASE_URLS = {
    "openai": "https://api.openai.com/v1",
    "openrouter": "https://openrouter.ai/api/v1",
    "gemini": "https://generativelanguage.googleapis.com/v1beta/openai/",
}
KEY_VARS = {
    "openai": "OPENAI_API_KEY",
    "openrouter": "OPENROUTER_API_KEY",
    "gemini": "GEMINI_API_KEY",
}


@dataclass
class Models:
    fast: str
    strong: str
    # Used when the provider keeps refusing a call (overloaded or over quota).
    fallback: str = ""


def provider_from_env() -> str:
    return os.getenv("LLM_PROVIDER", "openrouter").strip().lower()


def models_from_env() -> Models:
    """LLM_MODEL_FAST / LLM_MODEL_STRONG, falling back to the provider's own
    model variable (e.g. GEMINI_LLM_MODEL) when they are empty."""
    default = os.getenv(f"{provider_from_env().upper()}_LLM_MODEL", "")
    fast = os.getenv("LLM_MODEL_FAST", "") or default
    return Models(
        fast=fast,
        strong=os.getenv("LLM_MODEL_STRONG", "") or fast,
        fallback=os.getenv("LLM_FALLBACK_MODEL", ""),
    )


def client_from_env() -> LLMClient:
    """LLM_PROVIDER picks openai, openrouter or gemini. LLM_BASE_URL overrides
    the endpoint, e.g. for a local server."""
    provider = provider_from_env()
    if provider not in BASE_URLS:
        raise LLMError(f"unknown LLM_PROVIDER {provider!r}; use one of {sorted(BASE_URLS)}")
    base_url = os.getenv("LLM_BASE_URL") or BASE_URLS[provider]
    return OpenAICompatibleClient(
        base_url,
        os.getenv(KEY_VARS[provider], ""),
        provider=provider,
        reasoning_effort=os.getenv("LLM_REASONING_EFFORT", "low"),
    )
