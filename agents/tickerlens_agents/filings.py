"""Turn a 10-K's HTML into plain text split by Item (1 Business, 1A Risk
Factors, 7 MD&A, ...). Uses only the standard library's HTML parser."""

from __future__ import annotations

import re
from html.parser import HTMLParser

BLOCK_TAGS = {
    "p", "div", "br", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6",
    "table", "section", "article", "ul", "ol", "hr",
}  # fmt: skip
SKIP_TAGS = {"script", "style", "head", "title"}
CELL_TAGS = {"td", "th"}


class _TextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.skip_depth = 0  # inside script/style/hidden elements
        self.stack: list[bool] = []  # per open element: does it hide content?

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        style = (dict(attrs).get("style") or "").replace(" ", "").lower()
        hides = tag in SKIP_TAGS or "display:none" in style or tag == "ix:header"
        # Void elements never get an end tag; don't track them on the stack.
        if tag not in {"br", "hr", "img", "meta", "link", "input"}:
            self.stack.append(hides)
            if hides:
                self.skip_depth += 1
        if tag in BLOCK_TAGS:
            self.parts.append("\n")
        elif tag in CELL_TAGS:
            self.parts.append(" ")

    def handle_endtag(self, tag: str) -> None:
        if tag in {"br", "hr", "img", "meta", "link", "input"}:
            return
        if self.stack and self.stack.pop():
            self.skip_depth -= 1
        if tag in BLOCK_TAGS:
            self.parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self.skip_depth == 0:
            self.parts.append(data)


def html_to_text(html: str) -> str:
    p = _TextExtractor()
    p.feed(html)
    p.close()
    text = "".join(p.parts).replace("\xa0", " ")
    lines = (re.sub(r"[ \t\r\f\v]+", " ", line).strip() for line in text.split("\n"))
    out: list[str] = []
    for line in lines:
        if line or (out and out[-1]):  # keep at most one blank line
            out.append(line)
    return "\n".join(out).strip()


# "Item 1.", "ITEM 1A.", "Item 7 - Management's Discussion", "Item 1A: Risk Factors"
_DASHES = chr(0x2013) + chr(0x2014)  # en and em dash
_ITEM_RE = re.compile(rf"^item\s+(\d{{1,2}}[a-c]?)\s*[.:\-{_DASHES}]?\s*(.*)$", re.IGNORECASE)


def split_items(text: str) -> dict[str, str]:
    """Split filing text at "Item N." headings.

    The table of contents lists every item too, so an item can appear more
    than once; the real section is the occurrence with the most text before
    the next heading.
    """
    lines = text.split("\n")
    heads: list[tuple[int, str]] = []
    for i, line in enumerate(lines):
        if len(line) > 200:
            continue
        m = _ITEM_RE.match(line)
        if m:
            heads.append((i, m.group(1).upper()))

    best: dict[str, str] = {}
    for n, (start, item) in enumerate(heads):
        end = heads[n + 1][0] if n + 1 < len(heads) else len(lines)
        body = "\n".join(lines[start:end]).strip()
        if len(body) > len(best.get(item, "")):
            best[item] = body
    return best


def sections_from_html(html: str, max_chars: int = 60_000) -> dict[str, str]:
    """Item id → text, each truncated to max_chars (a token budget)."""
    items = split_items(html_to_text(html))
    return {k: v[:max_chars] for k, v in items.items()}


# --- Proxy statements (DEF 14A) ----------------------------------------------------

_OWNERSHIP_RE = re.compile(
    r"(security|stock|share)\s+ownership\s+of\s+(certain\s+)?(beneficial\s+owners|directors"
    r"|officers|management|executive)|beneficial\s+ownership\s+of\s+(common\s+stock|shares"
    r"|securities)",
    re.IGNORECASE,
)
_CEO_RE = re.compile(r"chief\s+executive\s+officer|\bCEO\b", re.IGNORECASE)
_YEAR_RE = re.compile(r"\b(19[5-9]\d|20[0-4]\d)\b")


def _ownership_window(text: str, size: int) -> str:
    """The ownership table: of the headings that name it (the table of
    contents names it too, and the 5% holders often have their own table),
    the one followed by the directors and officers "as a group" row and the
    most percentages."""
    starts = [m.start() for m in _OWNERSHIP_RE.finditer(text)]
    # Short heading lines that mention ownership ("Stock Ownership
    # Information" at Microsoft), other than ownership policies.
    pos = 0
    for line in text.split("\n"):
        low = line.lower()
        if len(line) <= 100 and "ownership" in low and not any(w in low for w in _POLICY_WORDS):
            starts.append(pos)
        pos += len(line) + 1
    best, best_score = "", -1
    for start in starts:
        window = text[start : start + size]
        head = window[:15000].lower()
        score = head.count("%")
        score += 100 if "beneficial" in head[:3000] else 0
        score += 200 if "officers as a group" in head else 0
        if score > best_score:
            best, best_score = window, score
    return best


_POLICY_WORDS = ("policy", "policies", "requirement", "guideline")


def _ceo_passages(text: str, size: int) -> str:
    """Sentences that mention the CEO together with a year: biographies
    ("has served as Chief Executive Officer since 2019")."""
    out: list[str] = []
    seen: set[str] = set()
    total = 0
    # Proxy text often breaks lines mid-sentence: rejoin each paragraph.
    paragraphs = (" ".join(p.split("\n")) for p in text.split("\n\n"))
    for paragraph in paragraphs:
        for sentence in re.split(r"(?<=[.;])\s+(?=[A-Z])", paragraph):
            if len(sentence) > 600 or not _CEO_RE.search(sentence) or not _YEAR_RE.search(sentence):
                continue
            key = sentence.strip().lower()
            if key in seen:
                continue
            seen.add(key)
            out.append(sentence.strip())
            total += len(sentence) + 1
            if total >= size:
                return "\n".join(out)
    return "\n".join(out)


def proxy_sections(html: str, max_chars: int = 30_000) -> dict[str, str]:
    """The parts of a proxy statement the management agent reads."""
    text = html_to_text(html)
    return {
        "ownership": _ownership_window(text, max_chars),
        "ceo": _ceo_passages(text, max_chars // 2),
    }
