"""Inter-service HTTP clients for the Document AI service.

Provides typed Python clients for:
- Auth service (token validation for protected endpoints)
- Election service (form template lookup)
- Compliance service (audit logging for document processing)
- Middleware service (event publishing for processed documents)
"""

import os
import logging
from typing import Any

import httpx

logger = logging.getLogger(__name__)

DEFAULT_TIMEOUT = 10.0


class ServiceClient:
    """Base HTTP client for inter-service communication."""

    def __init__(self, service_name: str, base_url: str):
        self.service_name = service_name
        self.base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=DEFAULT_TIMEOUT,
            headers={"X-Service-Name": service_name},
        )

    async def get(self, path: str) -> Any:
        resp = await self._client.get(path)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, body: Any) -> Any:
        resp = await self._client.post(path, json=body)
        resp.raise_for_status()
        return resp.json()

    async def health(self) -> bool:
        try:
            await self.get("/health")
            return True
        except Exception as e:
            logger.warning("%s health check failed: %s", self.service_name, e)
            return False

    async def close(self):
        await self._client.aclose()


class AuthServiceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("AUTH_URL", "http://localhost:8090")
        super().__init__("auth-svc", url)

    async def validate_token(self, token: str) -> dict:
        resp = await self._client.get(
            "/auth/me", headers={"Authorization": f"Bearer {token}"}
        )
        resp.raise_for_status()
        return resp.json()


class ElectionServiceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("ELECTION_URL", "http://localhost:8091")
        super().__init__("election-svc", url)

    async def get_form_template(self, form_type: str) -> dict:
        return await self.get(f"/elections/forms/{form_type}")


class ComplianceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("COMPLIANCE_URL", "http://localhost:8094")
        super().__init__("compliance-svc", url)

    async def log_document_processing(self, doc_id: str, action: str, result: str) -> dict:
        return await self.post(
            "/compliance/audit",
            {
                "action": action,
                "entity_type": "document",
                "entity_id": doc_id,
                "result": result,
            },
        )


class MiddlewareClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("MIDDLEWARE_URL", "http://localhost:8085")
        super().__init__("middleware-svc", url)

    async def publish_event(self, topic: str, key: str, value: Any) -> dict:
        return await self.post(
            "/kafka/publish", {"topic": topic, "key": key, "value": value}
        )


class ServiceRegistry:
    """Holds typed clients for all services Document AI talks to."""

    def __init__(self):
        self.auth = AuthServiceClient()
        self.election = ElectionServiceClient()
        self.compliance = ComplianceClient()
        self.middleware = MiddlewareClient()

    async def close(self):
        for client in [self.auth, self.election, self.compliance, self.middleware]:
            await client.close()

    async def health_check(self) -> dict[str, bool]:
        import asyncio
        checks = {
            "auth": self.auth.health(),
            "election": self.election.health(),
            "compliance": self.compliance.health(),
            "middleware": self.middleware.health(),
        }
        results = await asyncio.gather(*checks.values(), return_exceptions=True)
        return {name: (r is True) for name, r in zip(checks.keys(), results)}
