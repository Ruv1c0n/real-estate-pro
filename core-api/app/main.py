import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.config import settings
from app.logging_setup import setup_logging
from app.error_handlers import register_error_handlers

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(application: FastAPI) -> AsyncIterator[None]:
    setup_logging(settings.log_level)
    logger.info("Core API started")

    try:
        yield
    finally:
        logger.info("Core API stopped")


app = FastAPI(
    title=settings.app_name,
    version=settings.app_version,
    lifespan=lifespan,
)

register_error_handlers(app)

@app.get("/")
async def root() -> dict[str, str]:
    return {
        "service": "core-api",
        "status": "running",
    }