# CPU-only image for Azure Container Apps.
#
# Two-stage: the builder resolves wheels into a virtualenv, the runtime copies
# it. That keeps the toolchain and pip cache out of the shipped layer, which
# matters here because torch dominates the image size.
FROM python:3.12-slim AS builder

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /build
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

COPY requirements.txt ./

# CPU wheels first: the default index would pull the CUDA build of torch.
RUN pip install --index-url https://download.pytorch.org/whl/cpu torch==2.14.0 \
 && pip install -r requirements.txt

FROM python:3.12-slim AS runtime

ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    # Keep the model cache on a writable path owned by the app user.
    HF_HOME=/home/app/.cache/huggingface \
    PORT=8000

RUN useradd --create-home --uid 10001 app
COPY --from=builder /opt/venv /opt/venv

WORKDIR /app
COPY app/ ./app
COPY config/ ./config

USER app
EXPOSE 8000

# Container Apps probes this; a failing model load shows up here rather than as
# a stream of 500s on /decision.
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 \
    CMD python -c "import urllib.request,os,sys; sys.exit(0 if urllib.request.urlopen(f'http://127.0.0.1:{os.environ.get(\"PORT\",\"8000\")}/health', timeout=4).status == 200 else 1)"

# One worker: the base model is held in-process, so a second worker would
# double the memory for no throughput gain on a CPU-bound forward pass. Scale
# with Container Apps replicas instead.
CMD ["sh", "-c", "exec uvicorn app.server:app --host 0.0.0.0 --port ${PORT:-8000} --workers 1"]
