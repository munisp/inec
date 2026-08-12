"""Smoke tests: fail-closed auth, key rotation, and a probed health endpoint."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("document_ai_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    return TestClient(service.app)


def test_ocr_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "DOCUMENT_AI_API_KEYS", [])
    resp = client.post("/ocr/extract", files={"file": ("x.jpg", b"\xff\xd8\xff", "image/jpeg")})
    assert resp.status_code == 503


def test_ocr_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "DOCUMENT_AI_API_KEYS", ["doc-key-1", "doc-key-2"])
    files = {"file": ("x.jpg", b"\xff\xd8\xff", "image/jpeg")}
    assert client.post("/ocr/extract", files=files).status_code == 401
    assert (
        client.post("/ocr/extract", files=files, headers={"x-api-key": "bad"}).status_code
        == 401
    )


def test_rotated_key_passes_auth(client, monkeypatch):
    """Second (rotated) key authenticates; OCR then fails closed (no PaddleOCR in CI)."""
    monkeypatch.setattr(service, "DOCUMENT_AI_API_KEYS", ["doc-key-1", "doc-key-2"])
    resp = client.post(
        "/ocr/extract",
        files={"file": ("x.jpg", b"\xff\xd8\xff", "image/jpeg")},
        headers={"Authorization": "Bearer doc-key-2"},
    )
    # Auth passed (not 401/503-from-middleware); engine layer fails closed.
    assert resp.status_code not in (401,)


def test_health_has_real_probe_structure(client):
    resp = client.get("/health")
    # Engines unavailable in CI (no PaddleOCR/Docling) → degraded → 503.
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["services"], dict)
    assert "paddleocr" in body["services"]
    if resp.status_code == 503:
        assert body["status"] == "degraded"
