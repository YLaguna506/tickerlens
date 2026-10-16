"""Request and response shapes shared with the Go API (field names match the
JSON in PLAN.md Section 5, hence camelCase)."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field


class Document(BaseModel):
    id: str  # e.g. "10k-item1"
    source: str  # e.g. "10-K FY2025 Item 1"
    url: str = ""
    text: str


class AgentTask(BaseModel):
    jobId: str = ""
    section: str = ""
    ticker: str
    companyName: str = ""
    promptVersion: str = "v1"
    model: str = ""  # overrides the configured model
    metrics: dict[str, Any] = Field(default_factory=dict)
    documents: list[Document] = Field(default_factory=list)


class Evidence(BaseModel):
    claim: str = Field(description="The statement this quote supports")
    documentId: str = Field(description="Id of the document the quote comes from")
    quote: str = Field(description="Verbatim excerpt from that document, at most 30 words")


class UsageOut(BaseModel):
    model: str = ""
    inputTokens: int = 0
    outputTokens: int = 0
    costUsd: float = 0.0
    costKnown: bool = False


class AgentResult(BaseModel):
    jobId: str = ""
    section: str
    status: Literal["ok", "failed"]
    promptVersion: str
    output: dict[str, Any] | None = None
    evidence: list[Evidence] = Field(default_factory=list)
    usage: UsageOut = Field(default_factory=UsageOut)
    attempts: int = 0
    errors: list[str] = Field(default_factory=list)
