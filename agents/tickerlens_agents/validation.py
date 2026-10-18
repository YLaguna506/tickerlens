"""Checks every agent output must pass before it is accepted.

- Evidence: each quote must come from a provided document, verbatim, ≤ 40 words.
- Number guard: every number in the agent's prose must match a computed metric
  or a number inside its own quotes. Agents interpret numbers; they never
  invent them.
"""

from __future__ import annotations

import math
import re
from collections.abc import Iterable
from typing import Any

from pydantic import BaseModel

from tickerlens_agents.models import Document, Evidence

# The prompts ask for at most 30 words; a quote a few words over is still a
# quote, and not worth failing a whole section for (it happened on PepsiCo).
MAX_QUOTE_WORDS = 40

# Numbers like 1,234.5 / 40 / 12.5 / -3.86 ; the lookbehind skips digits glued
# to letters ("FY2025" is handled separately, "Q3" ignored).
_NUM_RE = re.compile(r"(?<![A-Za-z0-9.])-?\$?(\d{1,3}(?:,\d{3})+|\d+)(\.\d+)?")
# Form names are not numbers: 10-K, 10-Q, 8-K, Item 1A, Item 7.
_IGNORE_RE = re.compile(r"\b\d{1,2}-[KQ]\b|\b[Ii]tem\s+\d{1,2}[A-Ca-c]?\b")


def normalize(text: str) -> str:
    """Lower-case, unify quotes and dashes, drop footnote markers, and collapse
    everything that is not a letter or digit, so formatting differences do not
    break quote matching."""
    t = text.lower().translate(_UNIFY)
    t = _FOOTNOTE_RE.sub("", t)
    return re.sub(r"[^a-z0-9]+", " ", t).strip()


# A footnote marker glued to a word: "Net revenue(1)", "operations(2)". A
# negative amount like "$ (437)" is not glued to a word, so it stays.
_FOOTNOTE_RE = re.compile(r"(?<=[a-z])\(\d{1,2}\)")


# Curly quotes, en/em dashes and non-breaking spaces → plain ASCII.
_UNIFY = str.maketrans(
    {chr(0x2019): "'", chr(0x2018): "'", chr(0x201C): '"', chr(0x201D): '"',
     chr(0x2013): "-", chr(0x2014): "-", chr(0xA0): " "}
)  # fmt: skip


# A parenthetical aside in the filing: "Netflix, Inc. (“Netflix”, the “Company”)".
_PAREN_RE = re.compile(r"\s*\([^()]{1,200}\)")
# Where a quote marks words it left out.
_ELLIPSIS_RE = re.compile(r"\s*(?:\.\.\.|…)\s*")
MIN_PART_WORDS = 4


def _is_verbatim(quote: str, doc: str, doc_without_asides: str) -> bool:
    """The quote appears in the document word for word, allowing two harmless
    cuts: a parenthetical aside left out of the filing's sentence, and words
    left out where the quote says so with an ellipsis (each remaining piece
    must be at least MIN_PART_WORDS words, in the filing's order)."""
    parts = [normalize(p) for p in _ELLIPSIS_RE.split(quote)]
    parts = [p for p in parts if p]
    if not parts:
        return False
    if len(parts) > 1 and any(len(p.split()) < MIN_PART_WORDS for p in parts):
        return False
    for text in (doc, doc_without_asides):
        pos = 0
        for part in parts:
            found = text.find(part, pos)
            if found < 0:
                break
            pos = found + len(part)
        else:
            return True
    return False


def check_evidence(evidence: Iterable[Evidence], documents: list[Document]) -> list[str]:
    docs = {d.id: normalize(d.text) for d in documents}
    asides = {d.id: normalize(_PAREN_RE.sub("", d.text)) for d in documents}
    errors: list[str] = []
    for e in evidence:
        if e.documentId not in docs:
            errors.append(
                f"evidence cites unknown document {e.documentId!r}; use one of {sorted(docs)}"
            )
            continue
        words = len(e.quote.split())
        if words > MAX_QUOTE_WORDS:
            errors.append(f"quote has {words} words (max {MAX_QUOTE_WORDS}): {e.quote[:80]!r}")
        if not _is_verbatim(e.quote, docs[e.documentId], asides[e.documentId]):
            errors.append(f"quote is not verbatim from {e.documentId}: {e.quote[:120]!r}")
    return errors


def numbers_in(text: str) -> list[float]:
    """Numbers written in text (thousand separators removed)."""
    cleaned = _IGNORE_RE.sub(" ", text)
    out: list[float] = []
    for m in _NUM_RE.finditer(cleaned):
        raw = m.group(0).replace("$", "").replace(",", "")
        try:
            out.append(float(raw))
        except ValueError:
            continue
    # Years written as FY2025 / CY2024.
    out += [float(y) for y in re.findall(r"\b[FC]Y(\d{4})\b", text)]
    return out


def allowed_numbers(metrics: Any, quotes: Iterable[str]) -> list[float]:
    """Every number the agent may mention: computed metrics in the usual
    display scales (fraction→percent, dollars→millions/billions), numbers in
    metric labels and periods (years, dates), and numbers in its quotes."""
    values: list[float] = []

    def walk(v: Any) -> None:
        if isinstance(v, bool) or v is None:
            return
        if isinstance(v, int | float):
            f = float(v)
            values.extend([f, f * 100, f / 1e3, f / 1e6, f / 1e9, f / 1e12])
        elif isinstance(v, str):
            values.extend(numbers_in(v))
            values.extend(float(y) for y in re.findall(r"\b(\d{4})-\d{2}-\d{2}\b", v))
        elif isinstance(v, dict):
            for x in v.values():
                walk(x)
        elif isinstance(v, list):
            for x in v:
                walk(x)

    walk(metrics)
    for q in quotes:
        values.extend(numbers_in(q))
    return values


def _written(text: str) -> list[tuple[float, int]]:
    """Numbers in text with how many decimals were written ("11.3" → 1)."""
    out: list[tuple[float, int]] = []
    for m in _NUM_RE.finditer(_IGNORE_RE.sub(" ", text)):
        raw = m.group(0).replace("$", "").replace(",", "")
        try:
            out.append((float(raw), len(m.group(2) or ".") - 1))
        except ValueError:
            continue
    return out


def _matches(n: float, decimals: int, allowed: list[float]) -> bool:
    # A value rounded to the precision the agent wrote is fine: "11%" for
    # 11.3%, "61.1%" for 61.14%. Also allow 0.5% relative for large numbers.
    tol = 0.5 * 10**-decimals + 1e-9
    for a in allowed:
        if abs(n - a) <= tol or math.isclose(n, a, rel_tol=0.005):
            return True
        if abs(abs(n) - abs(a)) <= tol:  # sign written as words ("a loss of 1.3")
            return True
    return False


def check_numbers(texts: Iterable[str], allowed: list[float]) -> list[str]:
    errors: list[str] = []
    for text in texts:
        for n, decimals in _written(text):
            if n.is_integer() and 0 <= n <= 10:
                continue  # small counts ("two segments") are not data
            if not _matches(n, decimals, allowed):
                errors.append(
                    f"number {n:g} in {text[:100]!r} is not in the provided metrics "
                    "or your quotes; remove it or cite a quote that contains it"
                )
    return errors


def prose_fields(model: BaseModel) -> list[str]:
    """All string values of an output, except verbatim quotes."""
    out: list[str] = []

    def walk(v: Any, key: str = "") -> None:
        if isinstance(v, BaseModel):
            for k, x in v:
                walk(x, k)
        elif isinstance(v, str):
            if key != "quote":
                out.append(v)
        elif isinstance(v, list):
            for x in v:
                walk(x, key)

    walk(model)
    return out


def strict_schema(model: type[BaseModel]) -> dict[str, Any]:
    """JSON Schema for structured outputs: every property required and no extra
    properties, as OpenAI-style strict mode expects."""
    schema = model.model_json_schema()

    def fix(node: Any) -> None:
        if isinstance(node, dict):
            node.pop("default", None)
            node.pop("title", None)
            if node.get("type") == "object" and "properties" in node:
                node["additionalProperties"] = False
                node["required"] = list(node["properties"])
            for v in node.values():
                fix(v)
        elif isinstance(node, list):
            for v in node:
                fix(v)

    fix(schema)
    return schema
