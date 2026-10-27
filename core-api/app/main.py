from fastapi import FastAPI

app = FastAPI(
    title="Real Estate Pro — Core API",
    version="0.1.0",
)


@app.get("/")
async def root() -> dict[str, str]:
    return {
        "service": "core-api",
        "status": "running",
    }