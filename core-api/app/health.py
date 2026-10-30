import asyncio
import logging

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse
from redis.asyncio import Redis
from redis.exceptions import RedisError
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncEngine

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/health", tags=["Health"])

CHECK_TIMEOUT = 3


async def check_postgres(engine: AsyncEngine) -> bool:
    try:
        async with asyncio.timeout(CHECK_TIMEOUT):
            async with engine.connect() as connection:
                result = await connection.scalar(text("SELECT 1"))
                return result == 1
    except Exception as exc:
        logger.warning(
            "PostgreSQL readiness check failed: %s",
            type(exc).__name__,
        )
        logger.debug("PostgreSQL readiness check details", exc_info=True)
        return False


async def check_redis(client: Redis) -> bool:
    try:
        async with asyncio.timeout(CHECK_TIMEOUT):
            return bool(await client.ping())
    except (RedisError, TimeoutError, OSError) as exc:
        logger.warning(
            "Redis readiness check failed: %s",
            type(exc).__name__,
        )
        return False


async def check_dependencies(request: Request) -> tuple[bool, bool]:
    postgres_ok, redis_ok = await asyncio.gather(
        check_postgres(request.app.state.db_engine),
        check_redis(request.app.state.redis),
    )
    return postgres_ok, redis_ok


@router.get("", responses={503: {"description": "PostgreSQL недоступен"}})
async def health_summary(request: Request) -> JSONResponse:
    postgres_ok, redis_ok = await check_dependencies(request)

    if not postgres_ok:
        status = "unavailable"
    elif not redis_ok:
        status = "degraded"
    else:
        status = "ready"

    return JSONResponse(
        status_code=200 if postgres_ok else 503,
        content={
            "status": status,
            "checks": {
                "postgres": "ok" if postgres_ok else "unavailable",
                "redis": "ok" if redis_ok else "unavailable",
            },
        },
    )


@router.get("/live")
async def live() -> dict[str, str]:
    return {"status": "alive"}


@router.get(
    "/ready",
    responses={503: {"description": "Зависимости недоступны"}},
)
async def ready(request: Request) -> JSONResponse:
    postgres_ok, redis_ok = await check_dependencies(request)

    if not (postgres_ok and redis_ok):
        return JSONResponse(
            status_code=503,
            content={
                "error": "dependency_unavailable",
                "message": "Сервис временно не готов к работе",
            },
        )

    return JSONResponse(
        status_code=200,
        content={
            "status": "ready",
            "checks": {
                "postgres": "ok",
                "redis": "ok",
            },
        },
    )
