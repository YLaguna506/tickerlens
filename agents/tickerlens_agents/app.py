"""HTTP service for the TickerLens AI agents.

The Go API calls:
- POST /filings/sections with a 10-K's HTML, to get its text split by Item;
- POST /analyze/{section} with an AgentTask, for each report section.

The service is stateless: Go owns caching, retries across calls, and timeouts.
Every request carries the report id in X-Job-ID; each log line repeats it, so
these logs line up with the API's.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Awaitable, Callable
from contextvars import ContextVar
from functools import lru_cache
from typing import Annotated

from fastapi import Depends, FastAPI, HTTPException, Request, Response
from pydantic import BaseModel

from tickerlens_agents.agents import AGENTS, run_agent
from tickerlens_agents.filings import sections_from_html
from tickerlens_agents.llm import LLMClient, LLMError, client_from_env
from tickerlens_agents.llm.factory import Models, models_from_env
from tickerlens_agents.models import AgentResult, AgentTask

app = FastAPI(title="TickerLens agents")

job_id: ContextVar[str] = ContextVar("job_id", default="-")


class _JobFilter(logging.Filter):
    def filter(self, record: logging.LogRecord) -> bool:
        record.job = job_id.get()
        return True


log = logging.getLogger("tickerlens.agents")
if not log.handlers:
    _handler = logging.StreamHandler()
    _handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s job=%(job)s %(message)s"))
    log.addHandler(_handler)
    log.addFilter(_JobFilter())
    log.setLevel(logging.INFO)
    log.propagate = False


@app.middleware("http")
async def tag_with_job(
    request: Request, call_next: Callable[[Request], Awaitable[Response]]
) -> Response:
    """Reads X-Job-ID for the logs and logs each request with its duration."""
    token = job_id.set(request.headers.get("x-job-id", "-"))
    start = time.perf_counter()
    try:
        response = await call_next(request)
        if request.url.path != "/healthz":
            ms = (time.perf_counter() - start) * 1000
            path, status = request.url.path, response.status_code
            log.info("%s %s -> %d in %.0f ms", request.method, path, status, ms)
        return response
    finally:
        job_id.reset(token)


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
    result = run_agent(task, llm, models)
    u = result.usage
    log.info(
        "agent %s %s after %d attempt(s): model=%s tokens=%d+%d cost=%s",
        section, result.status, result.attempts, u.model, u.inputTokens, u.outputTokens,
        f"${u.costUsd:.4f}" if u.costKnown else "not reported",
    )  # fmt: skip
    return result
