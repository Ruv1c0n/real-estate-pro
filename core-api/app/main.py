import logging
from collections.abc import AsyncIterator
from contextlib import AsyncExitStack, asynccontextmanager

from fastapi import FastAPI

from app.config import settings
from app.logging_setup import setup_logging
from app.error_handlers import register_error_handlers
from app.database import create_db_engine
from app.redis_client import create_redis_client
from app.health import router as health_router


logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(application: FastAPI) -> AsyncIterator[None]:
    setup_logging(settings.log_level)

    async with AsyncExitStack() as stack:
        db_engine = create_db_engine(settings.database_url)
        stack.push_async_callback(db_engine.dispose)

        redis_client = create_redis_client(settings.redis_url)
        stack.push_async_callback(redis_client.aclose)

        application.state.db_engine = db_engine
        application.state.redis = redis_client

        logger.info("Core API started")
        try:
            yield
        finally:
            logger.info("Core API stopping")

    logger.info("Core API stopped")


app = FastAPI(
    title=settings.app_name,
    version=settings.app_version,
    lifespan=lifespan,
)

register_error_handlers(app)
app.include_router(health_router)

@app.get("/")
async def root() -> dict[str, str]:
    return {
        "service": "core-api",
        "status": "running",
    }