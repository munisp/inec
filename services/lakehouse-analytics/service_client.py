"""Inter-service HTTP clients for the Lakehouse Analytics service.

Provides typed Python clients for communicating with other INEC microservices:
- Auth service (token validation)
- Election service (election data)
- Geo service (spatial data for Sedona integration)
- Middleware service (event publishing, caching)
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

    async def list_elections(self) -> list[dict]:
        return await self.get("/elections")

    async def get_election(self, election_id: int) -> dict:
        return await self.get(f"/elections/{election_id}")

    async def get_results(self, election_id: int) -> list[dict]:
        return await self.get(f"/results?election_id={election_id}")


class GeoServiceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("GEO_URL", "http://localhost:8093")
        super().__init__("geo-svc", url)

    async def get_polling_units(self, state_code: str | None = None) -> list[dict]:
        path = "/geo/polling-units"
        if state_code:
            path += f"?state_code={state_code}"
        return await self.get(path)

    async def get_boundaries(self, level: str) -> dict:
        return await self.get(f"/geo/boundaries?level={level}")


class InferenceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("INFERENCE_URL", "http://localhost:8097")
        super().__init__("inference-engine", url)

    async def predict(self, model_name: str, features: list[float]) -> dict:
        return await self.post(
            f"/models/{model_name}/predict", {"features": features}
        )

    async def detect_anomaly(self, features: list[float]) -> dict:
        return await self.post("/predict/anomaly", {"features": features})


class MiddlewareClient(ServiceClient):
    """Client for the shared middleware service (Kafka, Redis, Temporal)."""

    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("MIDDLEWARE_URL", "http://localhost:8085")
        super().__init__("middleware-svc", url)

    async def publish_event(self, topic: str, key: str, value: Any) -> dict:
        return await self.post(
            "/kafka/publish", {"topic": topic, "key": key, "value": value}
        )

    async def cache_get(self, key: str) -> Any:
        result = await self.get(f"/cache/{key}")
        return result.get("value")

    async def cache_set(self, key: str, value: Any) -> None:
        await self._client.put(f"/cache/{key}", json={"value": value})

    async def start_workflow(
        self, workflow_id: str, workflow_type: str, input_data: Any
    ) -> dict:
        return await self.post(
            "/workflows/start",
            {
                "workflow_id": workflow_id,
                "workflow_type": workflow_type,
                "input": input_data,
            },
        )


class ComplianceClient(ServiceClient):
    def __init__(self, base_url: str | None = None):
        url = base_url or os.getenv("COMPLIANCE_URL", "http://localhost:8094")
        super().__init__("compliance-svc", url)

    async def log_audit(self, action: str, entity_type: str, entity_id: str) -> dict:
        return await self.post(
            "/compliance/audit",
            {"action": action, "entity_type": entity_type, "entity_id": entity_id},
        )


class ServiceRegistry:
    """Holds typed clients for all services the analytics service talks to."""

    def __init__(self):
        self.auth = AuthServiceClient()
        self.election = ElectionServiceClient()
        self.geo = GeoServiceClient()
        self.inference = InferenceClient()
        self.middleware = MiddlewareClient()
        self.compliance = ComplianceClient()

    async def close(self):
        for client in [
            self.auth, self.election, self.geo,
            self.inference, self.middleware, self.compliance,
        ]:
            await client.close()

    async def health_check(self) -> dict[str, bool]:
        import asyncio
        checks = {
            "auth": self.auth.health(),
            "election": self.election.health(),
            "geo": self.geo.health(),
            "inference": self.inference.health(),
            "middleware": self.middleware.health(),
            "compliance": self.compliance.health(),
        }
        results = await asyncio.gather(*checks.values(), return_exceptions=True)
        return {
            name: (r is True) for name, r in zip(checks.keys(), results)
        }
