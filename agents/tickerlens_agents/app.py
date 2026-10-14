"""HTTP service for the TickerLens AI agents.

The Go API calls `POST /analyze/{section}` for each report section (added in
M4). For now it only exposes a health check.
"""

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="TickerLens agents")


class Health(BaseModel):
    status: str


@app.get("/healthz")
def healthz() -> Health:
    return Health(status="ok")
