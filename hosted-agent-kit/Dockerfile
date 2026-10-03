# syntax=docker/dockerfile:1.7

# ---- build stage: resolve and install locked dependencies into a virtual environment ----
FROM python:3.13-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.11.7 /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/opt/venv
WORKDIR /build
COPY pyproject.toml uv.lock README.md ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-install-project --extra azure-monitor --extra service
COPY src ./src
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-editable --extra azure-monitor --extra service

# ---- runtime stage: no build tools, no package manager, non-root user ----
FROM python:3.13-slim AS runtime
RUN groupadd --system --gid 10001 pool \
 && useradd --system --uid 10001 --gid pool --no-create-home --shell /usr/sbin/nologin pool
# Apply Debian security fixes released after the base image was built, and remove pip and
# setuptools: the service runs from /opt/venv, and the copies of urllib3, msgpack and setuptools
# that pip bundles carry advisories that Trivy reports.
RUN set -eux; \
    apt-get update; \
    apt-get upgrade -y --no-install-recommends; \
    rm -rf /var/lib/apt/lists/*; \
    python -m pip uninstall -y pip setuptools wheel || true; \
    rm -rf /usr/local/lib/python3.13/site-packages/pip* \
           /usr/local/lib/python3.13/site-packages/setuptools* \
           /usr/local/lib/python3.13/site-packages/pkg_resources \
           /usr/local/lib/python3.13/site-packages/wheel*
COPY --from=build --chown=pool:pool /opt/venv /opt/venv
WORKDIR /app
# The agent pool configuration is read from AGENT_POOL_CONFIG, or from azure.yaml in /app.
COPY --chown=pool:pool azure.yaml ./azure.yaml

ENV PATH="/opt/venv/bin:${PATH}" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    POOL_HOST=0.0.0.0 \
    POOL_PORT=8080
USER 10001:10001
EXPOSE 8080

# Pool state is held in memory, so the service must run as exactly one process and one replica.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["python", "-c", "import sys, urllib.request; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8080/health/live', timeout=3).status == 200 else 1)"]

ENTRYPOINT ["hack-service"]
