FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py stakeholder_engine.py ./
EXPOSE 8206
CMD ["python", "main.py"]
