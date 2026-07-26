FROM apache/sedona:1.9.0

USER root
WORKDIR /app
COPY requirements.txt /tmp/requirements.txt
RUN python3 -m pip install --no-cache-dir -r /tmp/requirements.txt
COPY main.py /app/main.py
RUN useradd --system --uid 10001 --create-home inecsedona && chown -R inecsedona:inecsedona /app
USER inecsedona

ENV PYTHONUNBUFFERED=1
EXPOSE 8212
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8212/health', timeout=3)"
CMD ["python3", "-m", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8212"]
