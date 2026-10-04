# Container-mode build. The default deploy mode in azure.yaml is "code", which needs
# no Docker. Use this file only if you switch the service to `language: docker`.
FROM ghcr.io/astral-sh/uv:0.11.7 AS uv
FROM python:3.13-slim

COPY --from=uv /uv /uvx /bin/
WORKDIR /app/user_agent

# Install dependencies first so this layer is cached until the lock file changes.
COPY pyproject.toml uv.lock uv.toml ./
RUN uv sync --frozen --no-dev --no-install-project

COPY . .

# The hosted-agent contract: listen on 8088 and serve the health probe.
EXPOSE 8088

# Precompile bytecode at build time so cold starts do not pay for it. Dependencies are
# best-effort, but application code is compiled strictly so a syntax error fails the build.
RUN PYTHONDONTWRITEBYTECODE= .venv/bin/python -m compileall -q \
      $(.venv/bin/python -c "import sysconfig as s; print(s.get_paths()['purelib'])") || true; \
    PYTHONDONTWRITEBYTECODE= .venv/bin/python -m compileall -q -x '[.]venv' .

CMD ["/app/user_agent/.venv/bin/python", "main.py"]
