from tickerlens_agents.models import Document, Evidence
from tickerlens_agents.validation import (
    allowed_numbers,
    check_evidence,
    check_numbers,
    normalize,
    numbers_in,
)

RSQUO = chr(0x2019)  # curly apostrophe, as filings use
EMDASH = chr(0x2014)

DOC = Document(
    id="10k-item1",
    source="10-K FY2025 Item 1",
    text="Visa is one of the world" + RSQUO + "s leaders in digital payments. "
    "We facilitate transactions in more than 200 countries and territories.",
)


def ev(quote: str, doc: str = "10k-item1") -> Evidence:
    return Evidence(claim="c", documentId=doc, quote=quote)


def test_verbatim_quote_passes_despite_punctuation_differences() -> None:
    # Straight vs curly apostrophe must not matter.
    assert (
        check_evidence([ev("Visa is one of the world's leaders in digital payments")], [DOC]) == []
    )


def test_invented_quote_fails() -> None:
    errors = check_evidence([ev("Visa is the only payment network in the world")], [DOC])
    assert errors and "not verbatim" in errors[0]


def test_unknown_document_fails() -> None:
    errors = check_evidence([ev("Visa is one of", doc="10k-item7")], [DOC])
    assert errors and "unknown document" in errors[0]


def test_long_quote_fails() -> None:
    long = " ".join(["word"] * 31)
    errors = check_evidence([ev(long)], [DOC])
    assert any("max 30" in e for e in errors)


def test_numbers_in_ignores_form_names_and_reads_separators() -> None:
    assert numbers_in("Per the 10-K and Item 1A, revenue was $40,000 million, up 11.3%") == [
        40000.0,
        11.3,
    ]
    assert numbers_in("FY2025 results") == [2025.0]


def test_number_guard_accepts_metrics_in_display_scales() -> None:
    metrics = {"netMargin": 0.5168, "revenue": 43_027_000_000, "period": "FY2025"}
    allowed = allowed_numbers(metrics, [])
    text = "Net margin is 51.7% on $43.0 billion of revenue in FY2025."
    assert check_numbers([text], allowed) == []


def test_number_guard_rejects_hallucinated_number() -> None:
    allowed = allowed_numbers({"netMargin": 0.5168}, [])
    errors = check_numbers(["Net margin is 51.7% and market share is 61%."], allowed)
    assert len(errors) == 1 and "61" in errors[0]


def test_number_guard_accepts_numbers_from_own_quotes() -> None:
    allowed = allowed_numbers({}, ["in more than 200 countries and territories"])
    assert check_numbers(["Visa operates in over 200 countries."], allowed) == []


def test_number_guard_allows_rounding_to_written_precision() -> None:
    allowed = allowed_numbers({"revenueCagr1y": 0.113}, [])
    assert check_numbers(["Revenue grew 11% last year."], allowed) == []
    assert check_numbers(["Revenue grew 11.3% last year."], allowed) == []
    # One decimal written means one decimal of precision: 11.9 is not 11.3.
    assert check_numbers(["Revenue grew 11.9% last year."], allowed) != []


def test_number_guard_ignores_small_counts() -> None:
    assert check_numbers(["The company has 3 segments and 2 brands."], []) == []


def test_normalize() -> None:
    assert (
        normalize("World" + RSQUO + "s  LEADER " + EMDASH + " payments!")
        == "world s leader payments"
    )
