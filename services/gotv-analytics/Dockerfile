FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

# Install the package (pyproject declares every module in py-modules)
COPY pyproject.toml ./
COPY main.py middleware.py analytics_v2.py scoring_engine.py \
     koh_indicators.py ml_serving.py voting_analytics.py ./
RUN pip install --no-cache-dir .

RUN groupadd -r app && useradd -r -g app app && chown -R app:app /app
USER app

EXPOSE 8102
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8102"]
