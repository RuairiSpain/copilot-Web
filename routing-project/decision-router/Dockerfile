FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 PORT=5001
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt && useradd --create-home --uid 1001 router
COPY decision_router ./decision_router
COPY config ./config
USER router
EXPOSE 5001
CMD ["python", "-m", "decision_router"]
