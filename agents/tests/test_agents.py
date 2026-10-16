from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient

from tickerlens_agents.agents import run_agent
from tickerlens_agents.app import app, get_llm, get_models
from tickerlens_agents.llm import LLMError
from tickerlens_agents.llm.factory import Models
from tickerlens_agents.llm.fake import FakeClient
from tickerlens_agents.models import AgentTask, Document

ITEM1 = Document(
    id="10k-item1",
    source="10-K FY2025 Item 1",
    text=(
        "Visa is one of the world's leaders in digital payments. Our revenue is derived "
        "from fees paid by financial institutions and merchants for the use of our network. "
        "We do not issue cards, extend credit or set rates and fees for account holders."
    ),
)
MODELS = Models(fast="fast-model", strong="strong-model")


def task(section: str, metrics: dict[str, Any] | None = None) -> AgentTask:
    return AgentTask(
        jobId="job-1",
        section=section,
        ticker="V",
        companyName="VISA INC.",
        metrics=metrics or {"operatingMargin": 0.611},
        documents=[ITEM1],
    )


def overview(**over: Any) -> dict[str, Any]:
    out: dict[str, Any] = {
        "mission": None,
        "whatItDoes": "Visa runs a global digital payments network.",
        "howItMakesMoney": "It earns fees from banks and merchants that use its network.",
        "payingCustomers": "Financial institutions and merchants.",
        "whyTheyBuy": "Its network is accepted almost everywhere.",
        "evidence": [
            {
                "claim": "Revenue comes from network fees",
                "documentId": "10k-item1",
                "quote": "Our revenue is derived from fees paid by "
                "financial institutions and merchants",
            }
        ],
    }
    out.update(over)
    return out


def answer(value: str, quote: str = "We do not issue cards, extend credit") -> dict[str, Any]:
    return {
        "answer": value,
        "justification": "Operating margin of 61.1% reflects the network model.",
        "evidence": [{"claim": "c", "documentId": "10k-item1", "quote": quote}],
    }


def test_overview_ok() -> None:
    fake = FakeClient([overview()])
    result = run_agent(task("business_overview"), fake, MODELS)

    assert result.status == "ok", result.errors
    assert result.attempts == 1
    assert result.output and result.output["whatItDoes"].startswith("Visa")
    assert len(result.evidence) == 1
    assert result.usage.model == "fast-model"


def test_hallucinated_number_is_repaired_once() -> None:
    bad = overview(whatItDoes="Visa processes 300 billion transactions a year.")
    fake = FakeClient([bad, overview()])

    result = run_agent(task("business_overview"), fake, MODELS)

    assert result.status == "ok"
    assert result.attempts == 2
    # The repair request tells the model what was wrong.
    assert "300" in fake.calls[1][-1]["content"]


def test_fails_after_failed_repair() -> None:
    bad = overview(evidence=[{"claim": "x", "documentId": "10k-item1", "quote": "made up quote"}])
    result = run_agent(task("business_overview"), FakeClient([bad, bad]), MODELS)

    assert result.status == "failed"
    assert any("not verbatim" in e for e in result.errors)


def test_invalid_json_shape_is_reported() -> None:
    result = run_agent(task("business_overview"), FakeClient(["{}", "not json"]), MODELS)
    assert result.status == "failed" and result.errors


def test_business_quality_requires_evidence_per_answer() -> None:
    no_evidence = {**answer("Easily"), "evidence": []}
    out = {
        "revenuePredictability": answer("Predictable"),
        "pricingPower": no_evidence,
        "recessionResistance": answer("Strong"),
        "competitivePosition": answer("Dominant"),
    }
    result = run_agent(task("business_quality"), FakeClient([out, out]), MODELS)
    assert result.status == "failed"
    assert any("pricingPower" in e for e in result.errors)


def test_segments_numbers_must_be_quoted() -> None:
    doc = Document(
        id="10k-item7",
        source="Item 7",
        text="Service revenue 16,000 and data processing revenue 19,000.",
    )
    t = AgentTask(ticker="V", section="segments", documents=[doc])
    out = {
        "unit": "USD millions",
        "fiscalYear": "FY2025",
        "segments": [
            {"name": "Service", "revenue": 16000, "priorRevenue": None},
            {"name": "Data processing", "revenue": 19500, "priorRevenue": None},
        ],
        "geography": [],
        "evidence": [
            {
                "claim": "c",
                "documentId": "10k-item7",
                "quote": "Service revenue 16,000 and data processing revenue 19,000",
            }
        ],
    }
    result = run_agent(t, FakeClient([out, out]), MODELS)
    assert result.status == "failed"
    assert any("19500" in e for e in result.errors)


def test_segments_prior_year_must_be_quoted_too() -> None:
    doc = Document(
        id="10k-item7", source="Item 7", text="Service revenue was 17,535 versus 16,109."
    )
    t = AgentTask(ticker="V", section="segments", documents=[doc])
    quote = "Service revenue was 17,535 versus 16,109"
    ok = {
        "unit": "USD millions",
        "fiscalYear": "FY2025",
        "segments": [
            {"name": "Service", "revenue": 17535, "priorRevenue": 16109},
            {"name": "Data", "revenue": 17535, "priorRevenue": None},
        ],
        "geography": [],
        "evidence": [{"claim": "c", "documentId": "10k-item7", "quote": quote}],
    }
    assert run_agent(t, FakeClient([ok]), MODELS).status == "ok"
    bad = {**ok, "segments": [{"name": "Service", "revenue": 17535, "priorRevenue": 15000}]}
    result = run_agent(t, FakeClient([bad, bad]), MODELS)
    assert result.status == "failed" and any("15000" in e for e in result.errors)


def test_verdict_narrative_uses_strong_model_and_needs_no_documents() -> None:
    t = AgentTask(ticker="V", section="verdict_narrative", metrics={"verdict": "Hold", "growth": 4})
    out = {
        "tagline": "Great business, full price.",
        "rationale": "Growth scores 4 of 5. The price is high.",
    }
    result = run_agent(t, FakeClient([out]), MODELS)
    assert result.status == "ok" and result.usage.model == "strong-model"


@pytest.fixture
def client() -> Iterator[TestClient]:
    app.dependency_overrides[get_llm] = lambda: FakeClient([overview()])
    app.dependency_overrides[get_models] = lambda: MODELS
    yield TestClient(app)
    app.dependency_overrides.clear()


def test_analyze_endpoint(client: TestClient) -> None:
    resp = client.post("/analyze/business_overview", json=task("").model_dump())
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok" and body["section"] == "business_overview"


def test_analyze_unknown_section(client: TestClient) -> None:
    assert client.post("/analyze/nope", json=task("").model_dump()).status_code == 404


def test_filing_sections_endpoint(client: TestClient) -> None:
    html = (
        "<html><body><div style='display:none'>hidden xbrl</div>"
        "<p>Item 1. Business</p><p>1</p>"  # table of contents entry
        "<p>Item 1A. Risk Factors</p><p>2</p>"
        "<p>ITEM 1. Business</p><p>We run a payments network.</p><p>More detail.</p>"
        "<p>ITEM 1A. Risk Factors</p><p>Regulation could hurt us.</p>"
        "</body></html>"
    )
    resp = client.post("/filings/sections", content=html)
    items = resp.json()["items"]
    assert "We run a payments network." in items["1"]
    assert "Regulation" in items["1A"]
    assert "hidden" not in items["1"]


def test_falls_back_to_another_model_when_overloaded() -> None:
    class Overloaded(FakeClient):
        def complete_structured(self, messages, schema, schema_name, model):  # type: ignore[no-untyped-def]
            if model == "fast-model":
                raise LLMError("gemini: giving up after 4 attempts: HTTP 503: high demand")
            return super().complete_structured(messages, schema, schema_name, model)

    models = Models(fast="fast-model", strong="strong-model", fallback="backup-model")
    result = run_agent(task("business_overview"), Overloaded([overview()]), models)

    assert result.status == "ok", result.errors
    assert result.usage.model == "backup-model"


def test_bad_request_does_not_fall_back() -> None:
    class Rejects(FakeClient):
        def complete_structured(self, messages, schema, schema_name, model):  # type: ignore[no-untyped-def]
            raise LLMError("gemini: HTTP 400: invalid schema")

    models = Models(fast="fast-model", strong="strong-model", fallback="backup-model")
    result = run_agent(task("business_overview"), Rejects([]), models)
    assert result.status == "failed" and len(result.errors) == 1
