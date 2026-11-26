import asyncio
import logging
from collections.abc import AsyncIterator
from contextlib import AsyncExitStack, asynccontextmanager

from fastapi import FastAPI

from app.config import settings
from app.database import create_db_engine
from app.error_handlers import register_error_handlers
from app.health import router as health_router
from app.listing_body_limit import ListingBodyLimit
from app.listings import router as listings_router
from app.logging_setup import setup_logging
from app.outbox import run_outbox_worker
from app.postgres_listing_store import PostgresListingStore
from app.redis_client import create_redis_client
from app.reference_data import router as reference_data_router

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(application: FastAPI) -> AsyncIterator[None]:
    setup_logging(settings.log_level)

    async with AsyncExitStack() as stack:
        db_engine = create_db_engine(settings.database_url)
        stack.push_async_callback(db_engine.dispose)

        redis_client = create_redis_client(settings.redis_url)
        stack.push_async_callback(redis_client.aclose)

        store = PostgresListingStore(db_engine)
        application.state.db_engine = db_engine
        application.state.redis = redis_client
        application.state.listing_store = store

        stop = asyncio.Event()
        async with asyncio.TaskGroup() as tasks:
            tasks.create_task(
                run_outbox_worker(store, redis_client, stop),
                name="outbox-worker",
            )
            logger.info("Core API started")
            try:
                yield
            finally:
                logger.info("Core API stopping")
                stop.set()

        logger.info("Core API stopped")


app = FastAPI(
    title=settings.app_name,
    version=settings.app_version,
    lifespan=lifespan,
)

app.add_middleware(ListingBodyLimit)
register_error_handlers(app)
app.include_router(health_router)
app.include_router(listings_router)
app.include_router(reference_data_router)


@app.get("/")
async def root() -> dict[str, str]:
    return {
        "service": "core-api",
        "status": "running",
    }
