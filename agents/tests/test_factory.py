import pytest

from tickerlens_agents.llm import LLMError
from tickerlens_agents.llm.factory import available_models, client_from_env, models_from_env
from tickerlens_agents.llm.openai_compat import OpenAICompatibleClient


def test_models_fall_back_to_provider_model(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LLM_PROVIDER", "gemini")
    monkeypatch.setenv("GEMINI_LLM_MODEL", "gemini-3.5-flash")
    monkeypatch.setenv("LLM_MODEL_FAST", "")
    monkeypatch.setenv("LLM_MODEL_STRONG", "")
    m = models_from_env()
    assert (m.fast, m.strong) == ("gemini-3.5-flash", "gemini-3.5-flash")


def test_explicit_models_win(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LLM_PROVIDER", "gemini")
    monkeypatch.setenv("GEMINI_LLM_MODEL", "gemini-3.5-flash")
    monkeypatch.setenv("LLM_MODEL_FAST", "fast-one")
    monkeypatch.setenv("LLM_MODEL_STRONG", "strong-one")
    m = models_from_env()
    assert (m.fast, m.strong) == ("fast-one", "strong-one")


def test_gemini_client_uses_openai_compatible_endpoint(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LLM_PROVIDER", "gemini")
    monkeypatch.setenv("GEMINI_API_KEY", "test-key")
    monkeypatch.delenv("LLM_BASE_URL", raising=False)
    client = client_from_env()
    assert isinstance(client, OpenAICompatibleClient)
    assert str(client.http.base_url).startswith(
        "https://generativelanguage.googleapis.com/v1beta/openai"
    )


def test_missing_key_or_unknown_provider(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LLM_PROVIDER", "gemini")
    monkeypatch.setenv("GEMINI_API_KEY", "")
    with pytest.raises(LLMError):
        client_from_env()
    monkeypatch.setenv("LLM_PROVIDER", "nope")
    with pytest.raises(LLMError):
        client_from_env()


def test_available_models_only_lists_providers_with_keys(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LLM_PROVIDER", "gemini")
    monkeypatch.setenv("GEMINI_API_KEY", "g-key")
    monkeypatch.setenv("GEMINI_LLM_MODEL", "gemini-3.5-flash")
    monkeypatch.setenv("LLM_MODEL_FAST", "")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "gemini-3.5-flash-lite")
    monkeypatch.setenv("OPENROUTER_API_KEY", "o-key")
    monkeypatch.setenv("OPENROUTER_LLM_MODEL", "nvidia/nemotron:free")
    monkeypatch.setenv("OPENAI_API_KEY", "")
    monkeypatch.setenv("OPENAI_LLM_MODEL", "gpt-x")
    monkeypatch.setenv("LLM_MODEL_CHOICES", "gemini:gemini-3.5-pro, openai:gpt-y")

    ids = [(m.id, m.default) for m in available_models()]

    assert ids == [
        ("gemini:gemini-3.5-flash", True),
        ("gemini:gemini-3.5-flash-lite", False),
        ("openrouter:nvidia/nemotron:free", False),
        ("gemini:gemini-3.5-pro", False),
    ]
