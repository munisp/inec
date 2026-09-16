FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py .
EXPOSE 8201
CMD ["python", "main.py"]
# CI pipeline test 2026-09-16: confirms build-and-push -> DO registry -> Flux image automation end to end
