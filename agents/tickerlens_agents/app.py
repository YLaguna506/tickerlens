"""Health endpoints for the agent service.

The agents do their real work as NATS consumers (added in M4); this small
FastAPI app only lets Docker and operators check that the process is alive.
"""

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="TickerLens agents")


class Health(BaseModel):
    status: str


@app.get("/healthz")
def healthz() -> Health:
    return Health(status="ok")
