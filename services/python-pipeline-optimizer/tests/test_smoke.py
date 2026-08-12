"""Smoke tests: fail-closed engine state and a real health probe.

Note: this service authenticates at the gateway (APISIX) and has no in-process
API-key middleware, so these tests cover its fail-closed behavior when the
pipeline engine is not started (no DATABASE_URL -> lifespan refuses to start)
plus the health/metrics surface.
"""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("pipeline_optimizer_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    # No lifespan: startup requires DATABASE_URL + Kafka/Redis/Postgres, which
    # are deployment dependencies, not unit-test ones. pipeline_engine is None.
    return TestClient(service.app)


def test_ingest_fails_closed_when_engine_not_started(client):
    """Without a started engine, ingestion is refused (503), never queued."""
    resp = client.post("/api/v1/ingest", json=[{"id": 1}])
    assert resp.status_code == 503


def test_stats_fails_closed_when_engine_not_started(client):
    resp = client.get("/stats")
    assert resp.status_code == 200
    assert resp.json()["status"] == "not_started"


def test_startup_refuses_missing_database_url(monkeypatch):
    """Fail fast: the lifespan raises when DATABASE_URL is unconfigured."""
    monkeypatch.delenv("DATABASE_URL", raising=False)
    with pytest.raises(RuntimeError, match="DATABASE_URL is required"):
        with TestClient(service.app):
            pass


def test_health_returns_real_probe_structure(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json()["status"] == "healthy"
    metrics = client.get("/metrics")
    assert metrics.status_code == 200
