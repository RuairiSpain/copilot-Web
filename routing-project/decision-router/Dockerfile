FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 PORT=5001
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt && useradd --create-home --uid 1001 router
COPY decision_router ./decision_router
COPY config ./config
USER router
EXPOSE 5001
# REST API and MCP server (/mcp) on the same port; /health/live answers without credentials.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD python -c "import os, urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/health/live' % os.environ['PORT'], timeout=2)"
CMD ["python", "-m", "decision_router"]
