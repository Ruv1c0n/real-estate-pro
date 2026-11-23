"""POST /collect/run и статусы: 202/409/401, X-Admin-Key (ФТ-1.4, НФТ-3)."""

import asyncio
import time
from pathlib import Path

import httpx
import pytest
from conftest import make_raw, write_dump

from app.main import app as collector_app


@pytest.mark.anyio
async def test_collect_run_requires_admin_key(wired_app, make_settings, fake_redis):
    settings = make_settings(ADMIN_KEY="secret")
    write_dump(Path(settings.mock_dump_path), [make_raw("seed-1")])
    gate = asyncio.Event()

    async def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/api/v1/listings":
            await gate.wait()  # держим цикл активным для проверки 409
            return httpx.Response(201, json={"id": 1, "created": True})
        return httpx.Response(404, json={"error": "not_found", "message": "нет"})

    client = wired_app(settings, handler, fake_redis)

    response = await client.post("/collect/run")
    assert response.status_code == 401
    body = response.json()
    assert body["error"] == "unauthorized"
    assert "X-Admin-Key" in body["message"]

    response = await client.post("/collect/run", headers={"X-Admin-Key": "wrong"})
    assert response.status_code == 401

    response = await client.post("/collect/run", headers={"X-Admin-Key": "secret"})
    assert response.status_code == 202
    run_id = response.json()["run_id"]

    # активный запуск — 409 (ФТ-1.4)
    response = await client.post("/collect/run", headers={"X-Admin-Key": "secret"})
    assert response.status_code == 409
    assert response.json()["error"] == "run_in_progress"

    # статус читается с тем же ключом (НФТ-3)
    response = await client.get(
        f"/collect/runs/{run_id}", headers={"X-Admin-Key": "secret"}
    )
    assert response.status_code == 200
    assert response.json()["status"] == "running"

    gate.set()
    runner = collector_app.state.runner
    deadline = time.monotonic() + 3
    while runner.run_info(run_id).status == "running" and time.monotonic() < deadline:
        await asyncio.sleep(0.01)

    response = await client.get(
        f"/collect/runs/{run_id}", headers={"X-Admin-Key": "secret"}
    )
    assert response.status_code == 200
    assert response.json()["status"] == "completed"
    assert response.json()["created"] == 1

    response = await client.get(
        "/collect/runs/unknown", headers={"X-Admin-Key": "secret"}
    )
    assert response.status_code == 404


@pytest.mark.anyio
async def test_collect_run_without_key_open_in_dev(wired_app, make_settings):
    settings = make_settings(ADMIN_KEY="")  # dev-режим: доступ открыт

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(201, json={"id": 1, "created": True})

    client = wired_app(settings, handler)
    response = await client.post("/collect/run")
    assert response.status_code == 202
    assert response.json()["run_id"]
