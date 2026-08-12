FROM python:3.12-slim

WORKDIR /app
COPY pyproject.toml .
RUN pip install --no-cache-dir .

COPY . .

# Run as a non-root user; /data is a named volume the app writes to.
RUN useradd -u 10001 app && mkdir -p /data && chown app:app /data
USER app
EXPOSE 8090
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8090"]
