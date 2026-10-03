# Hosted agent container. Foundry requires the container to listen on port 8088.
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY agent.py .
EXPOSE 8088
CMD ["uvicorn", "agent:app", "--host", "0.0.0.0", "--port", "8088"]
