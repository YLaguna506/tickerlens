"""The analyst agents. Each one is a prompt, an output schema and its checks;
`run_agent` handles the LLM call, validation and one repair attempt."""

from __future__ import annotations

import json
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, Field, ValidationError

from tickerlens_agents.llm import LLMClient, LLMError, Usage
from tickerlens_agents.llm.factory import Models
from tickerlens_agents.models import AgentResult, AgentTask, Evidence, UsageOut
from tickerlens_agents.validation import (
    allowed_numbers,
    check_evidence,
    check_numbers,
    numbers_in,
    prose_fields,
    strict_schema,
)

PROMPTS_DIR = Path(__file__).resolve().parent.parent / "prompts"

# --- Output schemas ---------------------------------------------------------------


class BusinessOverview(BaseModel):
    mission: str | None = Field(
        description="The company's stated mission or purpose, or null if not stated"
    )
    whatItDoes: str = Field(description="One sentence")
    howItMakesMoney: str = Field(description="At most three sentences")
    payingCustomers: str = Field(description="Who pays the company")
    whyTheyBuy: str = Field(description="Why those customers choose it")
    evidence: list[Evidence]


class PredictabilityAnswer(BaseModel):
    answer: Literal["Unpredictable", "Modest", "Predictable"]
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class PricingAnswer(BaseModel):
    answer: Literal["No", "Sometimes", "Easily"]
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class RecessionAnswer(BaseModel):
    answer: Literal["Weak", "Okay", "Strong"]
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class CompetitiveAnswer(BaseModel):
    answer: Literal["Weak", "Average", "Dominant"]
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class BusinessQuality(BaseModel):
    revenuePredictability: PredictabilityAnswer
    pricingPower: PricingAnswer
    recessionResistance: RecessionAnswer
    competitivePosition: CompetitiveAnswer


class SegmentRow(BaseModel):
    name: str
    revenue: float = Field(description="As written in the filing, in the filing's unit")
    priorRevenue: float | None = Field(
        description="The previous fiscal year's figure, as written, or null if not shown"
    )


class Segments(BaseModel):
    unit: Literal["USD", "USD thousands", "USD millions", "USD billions"]
    fiscalYear: str = Field(description="The fiscal year these figures are for, e.g. FY2025")
    segments: list[SegmentRow] = Field(
        description="Revenue by business segment or product line; empty if not disclosed"
    )
    geography: list[SegmentRow] = Field(
        description="Revenue by geographic region; empty if not disclosed"
    )
    evidence: list[Evidence]


class VerdictNarrative(BaseModel):
    tagline: str = Field(description="One short line, e.g. 'Great business, fair price.'")
    rationale: str = Field(
        description="3-5 sentences explaining the verdict from the scores and justifications"
    )


Rating = Literal["None", "Weak", "Moderate", "Strong"]
SourceDirection = Literal["Narrowing", "Stable", "Widening"]


class MoatSource(BaseModel):
    rating: Rating
    direction: SourceDirection
    justification: str = Field(description="1-2 sentences")
    evidence: list[Evidence]


class MoatSources(BaseModel):
    switchingCosts: MoatSource
    networkEffects: MoatSource
    intangibleAssets: MoatSource = Field(description="Brands, patents, licenses, regulation")
    costAdvantage: MoatSource = Field(description="Low-cost production, scale, unique assets")
    counterPositioning: MoatSource = Field(
        description="A business model rivals cannot copy without hurting their own"
    )


class Moat(BaseModel):
    width: Literal["None", "Thin", "Narrow", "Broad", "Wide"]
    direction: Literal["Narrowing", "Slightly narrowing", "Stable", "Slightly widening", "Widening"]
    summary: str = Field(description="2-4 sentences, using the peer comparison where it helps")
    sources: MoatSources


RiskLevel = Literal["Severe", "High", "Moderate", "Low", "Minimal"]


class DiversificationAnswer(BaseModel):
    answer: Literal[
        "Very concentrated", "Concentrated", "Moderate", "Diversified", "Very diversified"
    ]
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class RiskAnswer(BaseModel):
    answer: RiskLevel
    justification: str = Field(description="1-3 sentences")
    evidence: list[Evidence]


class Risk(BaseModel):
    revenueDiversification: DiversificationAnswer
    disruptionThreat: RiskAnswer
    externalExposure: RiskAnswer = Field(
        description="Regulation, commodity prices, currencies, politics, single suppliers"
    )


class Management(BaseModel):
    ceoName: str
    ceoSince: int | None = Field(description="The year the CEO took the role, or null")
    sharesOwned: float | None = Field(
        description="Shares the CEO beneficially owns, as written in the ownership table"
    )
    percentOfClass: float | None = Field(
        description="The CEO's percent of the class as written, or null if shown as "
        "less than 1% (often an asterisk)"
    )
    lessThanOnePercent: bool
    directorsAndOfficersPercent: float | None = Field(
        description="All directors and executive officers as a group, percent as written"
    )
    notes: str = Field(
        description="1-2 sentences, e.g. what the ownership figure includes (options, trusts)"
    )
    evidence: list[Evidence]


# --- Agent-specific checks ----------------------------------------------------------


def _quality_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, BusinessQuality)
    errors = []
    for name, ans in out:
        if not ans.evidence:
            errors.append(f"{name}: cite at least one quote supporting the answer")
    return errors


def _overview_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, BusinessOverview)
    return [] if out.evidence else ["cite at least one quote from the filing"]


def _segments_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, Segments)
    errors = []
    quoted = [n for e in out.evidence for n in numbers_in(e.quote)]

    def quoted_value(v: float) -> bool:
        return any(abs(v - q) <= max(0.5, abs(q) * 0.001) for q in quoted)

    for row in out.segments + out.geography:
        if not quoted_value(row.revenue):
            errors.append(
                f"revenue {row.revenue:g} for {row.name!r} does not appear in any of your quotes"
            )
        if row.priorRevenue is not None and not quoted_value(row.priorRevenue):
            errors.append(
                f"prior-year revenue {row.priorRevenue:g} for {row.name!r} does not appear "
                "in any of your quotes; quote it or set it to null"
            )
    if (out.segments or out.geography) and not out.evidence:
        errors.append("cite the quotes the revenue figures come from")
    return errors


def _moat_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, Moat)
    errors = []
    sources = list(out.sources)
    for name, src in sources:
        if src.rating != "None" and not src.evidence:
            errors.append(f"sources.{name}: cite at least one quote for a {src.rating} rating")
    ratings = [src.rating for _, src in sources]
    if out.width in ("Broad", "Wide") and "Strong" not in ratings:
        errors.append(
            f"a {out.width} moat needs at least one Strong source; lower the width or a rating"
        )
    if out.width == "None" and any(r in ("Moderate", "Strong") for r in ratings):
        errors.append("width None contradicts a Moderate or Strong source")
    return errors


def _risk_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, Risk)
    return [f"{name}: cite at least one quote" for name, ans in out if not ans.evidence]


def _management_checks(out: BaseModel, task: AgentTask) -> list[str]:
    assert isinstance(out, Management)
    quotes = [e.quote for e in out.evidence]
    quoted = [n for q in quotes for n in numbers_in(q)]
    errors = []

    def check(label: str, v: float | None) -> None:
        if v is not None and not any(abs(v - q) <= max(0.5, abs(q) * 0.001) for q in quoted):
            errors.append(
                f"{label} {v:g} does not appear in any of your quotes; quote it or set it to null"
            )

    check("ceoSince", out.ceoSince)
    check("sharesOwned", out.sharesOwned)
    check("percentOfClass", out.percentOfClass)
    check("directorsAndOfficersPercent", out.directorsAndOfficersPercent)
    last = out.ceoName.split()[-1].lower() if out.ceoName.split() else ""
    if not any(last and last in q.lower() for q in quotes):
        errors.append(f"quote a passage that names the CEO ({out.ceoName})")
    if out.lessThanOnePercent and out.percentOfClass is not None and out.percentOfClass >= 1:
        errors.append("lessThanOnePercent contradicts percentOfClass")
    return errors


def _no_checks(out: BaseModel, task: AgentTask) -> list[str]:
    return []


@dataclass(frozen=True)
class AgentSpec:
    name: str
    output: type[BaseModel]
    model_tier: Literal["fast", "strong"]
    checks: Callable[[BaseModel, AgentTask], list[str]]
    needs_documents: bool = True


AGENTS: dict[str, AgentSpec] = {
    "business_overview": AgentSpec("business_overview", BusinessOverview, "fast", _overview_checks),
    "segments": AgentSpec("segments", Segments, "fast", _segments_checks),
    "business_quality": AgentSpec("business_quality", BusinessQuality, "fast", _quality_checks),
    "moat": AgentSpec("moat", Moat, "fast", _moat_checks),
    "risk": AgentSpec("risk", Risk, "fast", _risk_checks),
    "management": AgentSpec("management", Management, "fast", _management_checks),
    "verdict_narrative": AgentSpec(
        "verdict_narrative", VerdictNarrative, "strong", _no_checks, needs_documents=False
    ),
}


# --- Running an agent ---------------------------------------------------------------


def load_prompt(section: str, version: str) -> str:
    path = PROMPTS_DIR / section / f"{version}.md"
    if not path.is_file():
        raise FileNotFoundError(f"no prompt {section}/{version}.md")
    return path.read_text(encoding="utf-8")


LANGUAGE_NOTES = {
    "es": (
        "\n\nLanguage: write every free-text field (descriptions, justifications, claims, "
        "tagline, rationale) in Spanish, in a neutral Latin American register. Keep in "
        "English, exactly as the schema lists them: enum answers (for example "
        '"Predictable" or "Easily"), segment and region names, and every quote, which must '
        "stay word for word from the filing. Write numbers like the filing does, with a "
        "period for decimals (14.5%), so they can be checked."
    ),
}


def system_prompt(task: AgentTask) -> str:
    """The section's prompt, plus a language note when not writing in English."""
    return load_prompt(task.section, task.promptVersion) + LANGUAGE_NOTES.get(task.language, "")


def build_input(task: AgentTask) -> str:
    parts = [f"Company: {task.companyName} ({task.ticker})"]
    if task.metrics:
        header = "Computed metrics (JSON; the only numbers you may state besides your quotes):"
        parts.append(header + "\n" + json.dumps(task.metrics, separators=(",", ":")))
    for d in task.documents:
        parts.append(f'<document id="{d.id}" source="{d.source}">\n{d.text}\n</document>')
    return "\n\n".join(parts)


def all_evidence(out: BaseModel) -> list[Evidence]:
    found: list[Evidence] = []

    def walk(v: Any) -> None:
        if isinstance(v, Evidence):
            found.append(v)
        elif isinstance(v, BaseModel):
            for _, x in v:
                walk(x)
        elif isinstance(v, list):
            for x in v:
                walk(x)

    walk(out)
    return found


def validate(spec: AgentSpec, out: BaseModel, task: AgentTask) -> list[str]:
    evidence = all_evidence(out)
    errors = check_evidence(evidence, task.documents)
    allowed = allowed_numbers(task.metrics, [e.quote for e in evidence])
    errors += check_numbers(prose_fields(out), allowed)
    errors += spec.checks(out, task)
    return errors


def is_capacity_error(e: LLMError) -> bool:
    """Overloaded or over quota, as opposed to a bad request."""
    return any(code in str(e) for code in ("HTTP 429", "HTTP 503", "HTTP 502", "HTTP 500"))


def run_agent(
    task: AgentTask, client: LLMClient, models: Models, max_attempts: int = 2
) -> AgentResult:
    spec = AGENTS[task.section]
    result = AgentResult(
        jobId=task.jobId, section=task.section, status="failed", promptVersion=task.promptVersion
    )
    if spec.needs_documents and not task.documents:
        result.errors = ["no filing documents provided"]
        return result

    model = task.model or (models.strong if spec.model_tier == "strong" else models.fast)
    schema = strict_schema(spec.output)
    messages = [
        {"role": "system", "content": system_prompt(task)},
        {"role": "user", "content": build_input(task)},
    ]
    usage = Usage(model=model)

    for attempt in range(1, max_attempts + 1):
        result.attempts = attempt
        try:
            reply = client.complete_structured(messages, schema, spec.name, model)
        except LLMError as e:
            # The provider kept refusing (overloaded / over quota): switch to
            # the fallback model once and try this attempt again with it.
            if models.fallback and model != models.fallback and is_capacity_error(e):
                model = models.fallback
                try:
                    reply = client.complete_structured(messages, schema, spec.name, model)
                except LLMError as e2:
                    result.errors = [str(e), f"fallback {model}: {e2}"]
                    break
            else:
                result.errors = [str(e)]
                break
        usage.add(reply.usage)
        if not reply.text.strip():
            errors = ["your reply was empty; return the JSON object only"]
            result.errors = errors
            messages += [{"role": "user", "content": errors[0]}]
            continue
        try:
            out = spec.output.model_validate_json(reply.text)
        except ValidationError as e:
            errors = [
                f"{'.'.join(str(p) for p in err['loc'])}: {err['msg']}" for err in e.errors()[:10]
            ]
        else:
            errors = validate(spec, out, task)
            if not errors:
                result.status = "ok"
                result.output = (
                    out.model_dump(exclude={"evidence"})
                    if "evidence" in spec.output.model_fields
                    else out.model_dump()
                )
                result.evidence = all_evidence(out)
                result.errors = []
                break
        result.errors = errors
        # One repair attempt: show the model exactly what was wrong.
        messages += [
            {"role": "assistant", "content": reply.text},
            {
                "role": "user",
                "content": "Your answer failed validation:\n- "
                + "\n- ".join(errors)
                + "\nReturn the corrected JSON only.",
            },
        ]

    result.usage = UsageOut(
        model=usage.model,
        inputTokens=usage.input_tokens,
        outputTokens=usage.output_tokens,
        costUsd=round(usage.cost_usd, 6),
        costKnown=usage.cost_known,
    )
    return result
