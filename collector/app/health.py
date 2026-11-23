"""Health-эндпоинты collector'а (НФТ-8): /health — живость; /health/ready —
готовность (Core API и Redis). Справочники отражаются в ответе, но
готовность не валят: нормализация деградирует, а не останавливается."""

import logging
from datetime import UTC, datetime

import httpx
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

logger = logging.getLogger(__name__)

router = APIRouter(tags=["health"])

_PROBE_TIMEOUT_SECONDS = 2.0


def _now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


@router.get("/health")
async def live() -> dict[str, str]:
    return {"service": "collector", "status": "ok", "time": _now_iso()}


@router.get("/health/ready")
async def ready(request: Request) -> JSONResponse:
    settings = request.app.state.settings
    core_ok = await _probe(request.app.state.http, f"{settings.core_api_url}/health")
    redis_ok = await _probe_redis(request.app.state.redis)
    dicts_ok = request.app.state.dicts.snapshot is not None
    ready_ok = core_ok and redis_ok
    return JSONResponse(
        {
            "status": "ok" if ready_ok else "degraded",
            "core_api": core_ok,
            "redis": redis_ok,
            "dicts": dicts_ok,
        },
        status_code=200 if ready_ok else 503,
    )


async def _probe(client: httpx.AsyncClient, url: str) -> bool:
    """200 у /health Core API — он жив вместе со своими зависимостями."""
    try:
        response = await client.get(url, timeout=_PROBE_TIMEOUT_SECONDS)
    except httpx.HTTPError:
        return False
    return response.status_code == 200


async def _probe_redis(redis_client) -> bool:
    try:
        await redis_client.ping()
    except Exception:
        return False
    return True
