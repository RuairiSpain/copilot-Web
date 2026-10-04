# Container-mode build (azure.yaml: language: docker).
# Unlike the code-mode agents, this one shells out to git and pytest at runtime, so the
# image installs git explicitly instead of relying on the managed runtime.
FROM ghcr.io/astral-sh/uv:0.11.7 AS uv
FROM python:3.13-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

COPY --from=uv /uv /uvx /bin/
WORKDIR /app/user_agent

COPY pyproject.toml uv.lock uv.toml ./
RUN uv sync --frozen --no-dev --no-install-project

COPY . .

# The hosted-agent contract: listen on 8088 and serve the health probe.
EXPOSE 8088

RUN PYTHONDONTWRITEBYTECODE= .venv/bin/python -m compileall -q -x '[.]venv' .

CMD ["/app/user_agent/.venv/bin/python", "main.py"]
