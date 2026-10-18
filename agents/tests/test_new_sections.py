"""Moat, risk and management agents, and the proxy statement excerpts."""

from typing import Any

from fastapi.testclient import TestClient

from tickerlens_agents.agents import run_agent
from tickerlens_agents.app import app
from tickerlens_agents.filings import proxy_sections
from tickerlens_agents.llm.factory import Models
from tickerlens_agents.llm.fake import FakeClient
from tickerlens_agents.models import AgentTask, Document

MODELS = Models(fast="fast-model", strong="strong-model")

ITEM1 = Document(
    id="10k-item1",
    source="10-K Item 1",
    text=(
        "Visa is one of the world's leaders in digital payments. Our network connects "
        "billions of cards and millions of merchants, and each new merchant makes the "
        "network more useful for cardholders. We face intense competition from new "
        "payment methods."
    ),
)
PROXY = Document(
    id="proxy-ownership",
    source="Proxy statement, ownership table",
    text=(
        "Stock Ownership of Officers and Directors\nRyan McInerney 245,118 *\n"
        "All directors and executive officers as a group (18 persons) 1,201,333 *\n"
        "McInerney has served as our Chief Executive Officer since 2023."
    ),
)


def task(section: str, *docs: Document) -> AgentTask:
    return AgentTask(
        jobId="job-1",
        section=section,
        ticker="V",
        companyName="VISA INC.",
        metrics={"operatingMargin": 0.611, "peerMedianOperatingMargin": 0.215},
        documents=list(docs) or [ITEM1],
    )


def source(
    rating: str, quote: str = "each new merchant makes the network more useful"
) -> dict[str, Any]:
    return {
        "rating": rating,
        "direction": "Stable",
        "justification": "Operating margin of 61.1% against a peer median of 21.5%.",
        "evidence": []
        if rating == "None"
        else [{"claim": "c", "documentId": "10k-item1", "quote": quote}],
    }


def moat(width: str, network: str = "Strong", **sources: str) -> dict[str, Any]:
    names = ["switchingCosts", "intangibleAssets", "costAdvantage", "counterPositioning"]
    return {
        "width": width,
        "direction": "Stable",
        "summary": "Margins well above peers point to a durable network.",
        "sources": {"networkEffects": source(network)}
        | {n: source(sources.get(n, "None")) for n in names},
    }


def test_moat_ok() -> None:
    result = run_agent(task("moat"), FakeClient([moat("Wide")]), MODELS)
    assert result.status == "ok", result.errors
    assert result.output and result.output["width"] == "Wide"


def test_wide_moat_needs_a_strong_source() -> None:
    weak = moat("Wide", network="Moderate")
    result = run_agent(task("moat"), FakeClient([weak, weak]), MODELS)
    assert result.status == "failed"
    assert any("needs at least one Strong source" in e for e in result.errors)


def test_moat_source_rating_needs_evidence() -> None:
    out = moat("Narrow", network="Moderate")
    out["sources"]["networkEffects"]["evidence"] = []
    result = run_agent(task("moat"), FakeClient([out, out]), MODELS)
    assert any("networkEffects" in e for e in result.errors)


def risk_answer(value: str) -> dict[str, Any]:
    return {
        "answer": value,
        "justification": "Competition from new payment methods.",
        "evidence": [
            {
                "claim": "c",
                "documentId": "10k-item1",
                "quote": "We face intense competition from new payment methods",
            }
        ],
    }


def test_risk_ok() -> None:
    out = {
        "revenueDiversification": risk_answer("Diversified"),
        "disruptionThreat": risk_answer("Moderate"),
        "externalExposure": risk_answer("High"),
    }
    result = run_agent(task("risk"), FakeClient([out]), MODELS)
    assert result.status == "ok", result.errors


def management(**over: Any) -> dict[str, Any]:
    out: dict[str, Any] = {
        "ceoName": "Ryan McInerney",
        "ceoSince": 2023,
        "sharesOwned": 245118,
        "percentOfClass": None,
        "lessThanOnePercent": True,
        "directorsAndOfficersPercent": None,
        "notes": "The table shows an asterisk, meaning less than 1%.",
        "evidence": [
            {
                "claim": "shares",
                "documentId": "proxy-ownership",
                "quote": "Ryan McInerney 245,118 *",
            },
            {
                "claim": "since",
                "documentId": "proxy-ownership",
                "quote": "McInerney has served as our Chief Executive Officer since 2023.",
            },
        ],
    }
    out.update(over)
    return out


def test_management_ok() -> None:
    result = run_agent(task("management", PROXY), FakeClient([management()]), MODELS)
    assert result.status == "ok", result.errors


def test_management_numbers_must_be_quoted() -> None:
    bad = management(sharesOwned=999999)
    result = run_agent(task("management", PROXY), FakeClient([bad, bad]), MODELS)
    assert any("sharesOwned 999999" in e for e in result.errors)


def test_proxy_sections_find_the_officers_table_and_ceo() -> None:
    html = (
        "<p>Stock Ownership of Officers and Directors</p><p>96</p>"  # table of contents
        + "<p>filler</p>" * 50
        + "<p>Stock Ownership of Certain Beneficial Owners</p><p>Vanguard 10.1%</p>"
        + "<p>filler</p>" * 50
        + "<p>Stock Ownership of Officers and Directors</p><p>Jane Doe 1,000</p>"
        + "<p>All directors and executive officers as a group 5,000</p>"
        + "<p>Jane Doe has served as our Chief Executive\nOfficer since 2019. She likes tea.</p>"
    )
    p = proxy_sections(html, max_chars=200)
    assert "as a group" in p["ownership"]
    assert "Jane Doe has served as our Chief Executive Officer since 2019." in p["ceo"]
    assert "tea" not in p["ceo"]


def test_proxy_endpoint() -> None:
    resp = TestClient(app).post(
        "/filings/proxy", content="<p>Stock Ownership of Directors</p><p>x</p>"
    )
    assert resp.status_code == 200
    assert set(resp.json()["items"]) == {"ownership", "ceo"}
