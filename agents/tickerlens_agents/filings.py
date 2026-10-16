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
