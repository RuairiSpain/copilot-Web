FROM python:3.13-slim AS build
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt
COPY pyproject.toml ./
COPY src ./src
RUN pip install --no-cache-dir --prefix=/install --no-deps .

FROM python:3.13-slim
ENV PYTHONUNBUFFERED=1 PORT=8000
RUN useradd --create-home --uid 10001 app
COPY --from=build /install /usr/local
USER app
EXPOSE 8000
# Inside Azure, DefaultAzureCredential uses the managed identity of the host.
# Pass FOUNDRY_PROJECT_ENDPOINT, VOICE_AGENT_NAME, ACS_CONNECTION_NAME and
# RESOURCE_ACCOUNT_OBJECT_ID as environment variables (see .env.example).
HEALTHCHECK --interval=30s --timeout=3s CMD python -c "import os,urllib.request as u; u.urlopen('http://127.0.0.1:%s/healthz' % os.environ['PORT'])"
CMD ["python", "-m", "teams_voice_meeting"]
