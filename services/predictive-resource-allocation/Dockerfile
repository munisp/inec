FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py .
# Run as a non-root user; /data is a named volume the app writes to.
RUN useradd -u 10001 app && mkdir -p /data && chown app:app /data
USER app
EXPOSE 8205
CMD ["python", "main.py"]
