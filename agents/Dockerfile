FROM python:3.12.15-slim
COPY --from=ghcr.io/astral-sh/uv:0.13.1 /uv /bin/uv

WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PATH="/app/.venv/bin:$PATH"

# Install dependencies first so this layer is cached when only code changes.
COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project

COPY tickerlens_agents ./tickerlens_agents
RUN uv sync --locked --no-dev

RUN useradd --create-home appuser
USER appuser

EXPOSE 8000
CMD ["uvicorn", "tickerlens_agents.app:app", "--host", "0.0.0.0", "--port", "8000"]
