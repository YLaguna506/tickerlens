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
from tickerlens_agents.llm.factory import (
    Models,
    available_models,
    client_for,
    models_from_env,
    provider_from_env,
)
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


def get_client_for() -> Callable[[str], LLMClient | None]:
    """A lookup of other providers' clients, for tasks that pick a model."""

    def lookup(provider: str) -> LLMClient | None:
        try:
            return client_for(provider)
        except LLMError:
            return None

    return lookup


class ModelOut(BaseModel):
    id: str
    provider: str
    model: str
    default: bool


class ModelList(BaseModel):
    models: list[ModelOut]


@app.get("/models")
def list_models() -> ModelList:
    """Models a report can use: providers with a key set, default first."""
    return ModelList(models=[ModelOut(**vars(m)) for m in available_models()])


def get_models() -> Models:
    return models_from_env()


@app.post("/analyze/{section}")
def analyze(
    section: str,
    task: AgentTask,
    llm: Annotated[LLMClient | None, Depends(get_llm)],
    models: Annotated[Models, Depends(get_models)],
    client_for_provider: Annotated[Callable[[str], LLMClient | None], Depends(get_client_for)],
) -> AgentResult:
    if section not in AGENTS:
        raise HTTPException(
            status_code=404, detail=f"unknown section {section!r}; known: {sorted(AGENTS)}"
        )
    task.section = section
    if task.provider and task.provider != provider_from_env():
        # Another provider's model: its own client, and no fallback (the
        # configured fallback belongs to the default provider).
        llm = client_for_provider(task.provider)
        models = Models(fast=task.model, strong=task.model)
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
