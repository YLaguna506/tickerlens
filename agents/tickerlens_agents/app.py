"""HTTP service for the TickerLens AI agents.

The Go API calls:
- POST /filings/sections with a 10-K's HTML, to get its text split by Item;
- POST /analyze/{section} with an AgentTask, for each report section.

The service is stateless: Go owns caching, retries across calls, and timeouts.
"""

from __future__ import annotations

from functools import lru_cache
from typing import Annotated

from fastapi import Depends, FastAPI, HTTPException, Request
from pydantic import BaseModel

from tickerlens_agents.agents import AGENTS, run_agent
from tickerlens_agents.filings import sections_from_html
from tickerlens_agents.llm import LLMClient, LLMError, client_from_env
from tickerlens_agents.llm.factory import Models, models_from_env
from tickerlens_agents.models import AgentResult, AgentTask

app = FastAPI(title="TickerLens agents")


class Health(BaseModel):
    status: str


@app.get("/healthz")
def healthz() -> Health:
    return Health(status="ok")


class Sections(BaseModel):
    items: dict[str, str]


@app.post("/filings/sections")
async def filing_sections(request: Request) -> Sections:
    """Body: the filing's raw HTML. Returns its text split by Item."""
    html = (await request.body()).decode("utf-8", errors="replace")
    if not html.strip():
        raise HTTPException(status_code=400, detail="empty body: send the filing HTML")
    return Sections(items=sections_from_html(html))


# Dependencies are functions so tests can swap them (app.dependency_overrides).
@lru_cache
def get_llm() -> LLMClient | None:
    try:
        return client_from_env()
    except LLMError:
        return None


def get_models() -> Models:
    return models_from_env()


@app.post("/analyze/{section}")
def analyze(
    section: str,
    task: AgentTask,
    llm: Annotated[LLMClient | None, Depends(get_llm)],
    models: Annotated[Models, Depends(get_models)],
) -> AgentResult:
    if section not in AGENTS:
        raise HTTPException(
            status_code=404, detail=f"unknown section {section!r}; known: {sorted(AGENTS)}"
        )
    task.section = section
    if llm is None:
        return AgentResult(
            jobId=task.jobId, section=section, status="failed", promptVersion=task.promptVersion,
            errors=["LLM not configured: set LLM_PROVIDER and its API key"],
        )  # fmt: skip
    return run_agent(task, llm, models)
