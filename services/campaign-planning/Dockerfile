FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py stakeholder_engine.py ./
# Run as a non-root user.
RUN useradd -u 10001 app
USER app
EXPOSE 8206
CMD ["python", "main.py"]
