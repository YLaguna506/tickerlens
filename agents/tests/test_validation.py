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
    long = " ".join(["word"] * 41)
    errors = check_evidence([ev(long)], [DOC])
    assert any("max 40" in e for e in errors)


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


def test_quotes_ignore_footnote_markers() -> None:
    text = "Net revenue(1)\n$ 34,296 $ 17,745 and operating losses $ (437) in total"
    doc = Document(id="10k-item7", source="Item 7", text=text)
    footnote = Evidence(claim="c", documentId="10k-item7", quote="Net revenue $ 34,296 $ 17,745")
    # A negative amount in parentheses is not a footnote: it must stay.
    negative = Evidence(
        claim="c", documentId="10k-item7", quote="operating losses $ (437) in total"
    )
    assert check_evidence([footnote, negative], [doc]) == []
    assert normalize("losses $ (437)") == "losses 437"


def test_normalize() -> None:
    assert (
        normalize("World" + RSQUO + "s  LEADER " + EMDASH + " payments!")
        == "world s leader payments"
    )


NFLX = (
    "Netflix, Inc. (\u201cNetflix\u201d, the \u201cCompany\u201d, or \u201cus\u201d) "
    "is one of the world\u2019s leading entertainment services offering TV series, "
    "films, games and live programming "
    "across a wide variety of genres and languages. Members can play, pause and resume."
)


def nflx(quote: str) -> Evidence:
    return Evidence(claim="c", documentId="10k-item1", quote=quote)


def test_quotes_may_leave_out_a_parenthetical_or_use_an_ellipsis() -> None:
    doc = Document(id="10k-item1", source="Item 1", text=NFLX)
    ok = [
        nflx(
            "Netflix, Inc. is one of the world's leading entertainment services offering TV series"
        ),
        nflx(
            "Netflix, Inc. is one of the world's leading entertainment services"
            " … across a wide variety of genres"
        ),
        nflx(
            "one of the world's leading entertainment services..."
            " Members can play, pause and resume"
        ),
    ]
    assert check_evidence(ok, [doc]) == []


def test_ellipsis_does_not_allow_made_up_or_reordered_text() -> None:
    doc = Document(id="10k-item1", source="Item 1", text=NFLX)
    bad = [
        nflx("Netflix, Inc. is the world's largest entertainment service"),  # invented
        nflx(
            "Members can play, pause and resume … leading entertainment services offering TV"
        ),  # reordered
        nflx("Netflix … films … languages"),  # pieces too short to mean anything
        nflx(
            "Netflix, Inc. (the best company) is one of the world's leading entertainment services"
        ),  # fake aside
    ]
    assert len(check_evidence(bad, [doc])) == len(bad)
