"""Build the configured LLM client from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass
from functools import lru_cache

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
    return client_for(provider_from_env())


def client_for(provider: str) -> LLMClient:
    """The client for one provider. LLM_BASE_URL applies only to the default
    provider. Clients are reused for the same settings."""
    if provider not in BASE_URLS:
        raise LLMError(f"unknown LLM provider {provider!r}; use one of {sorted(BASE_URLS)}")
    base_url = BASE_URLS[provider]
    if provider == provider_from_env():
        base_url = os.getenv("LLM_BASE_URL") or base_url
    key = os.getenv(KEY_VARS[provider], "")
    return _client(provider, base_url, key, os.getenv("LLM_REASONING_EFFORT", "low"))


@lru_cache
def _client(provider: str, base_url: str, key: str, effort: str) -> LLMClient:
    return OpenAICompatibleClient(base_url, key, provider=provider, reasoning_effort=effort)


@dataclass
class ModelChoice:
    id: str  # "provider:model"
    provider: str
    model: str
    default: bool = False


def available_models() -> list[ModelChoice]:
    """The models a report can use, default first: the configured model, its
    fallback, each other provider's <PROVIDER>_LLM_MODEL when its key is set,
    and anything listed in LLM_MODEL_CHOICES ("provider:model, ...")."""
    default_provider = provider_from_env()
    candidates: list[tuple[str, str]] = []
    configured = models_from_env()
    candidates.append((default_provider, configured.fast))
    if configured.fallback:
        candidates.append((default_provider, configured.fallback))
    for provider in BASE_URLS:
        if provider != default_provider:
            candidates.append((provider, os.getenv(f"{provider.upper()}_LLM_MODEL", "")))
    for item in os.getenv("LLM_MODEL_CHOICES", "").split(","):
        provider, _, model = item.strip().partition(":")
        candidates.append((provider.strip().lower(), model.strip()))

    out: list[ModelChoice] = []
    seen: set[str] = set()
    for provider, model in candidates:
        key = f"{provider}:{model}"
        if (
            not model
            or provider not in KEY_VARS
            or not os.getenv(KEY_VARS[provider])
            or key in seen
        ):
            continue
        seen.add(key)
        out.append(ModelChoice(id=key, provider=provider, model=model, default=not out))
    return out
