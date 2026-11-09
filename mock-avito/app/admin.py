"""Admin API и HTMX-консоль управления mock-avito.

Ручки защищены заголовком X-Admin-Key (MOCK_ADMIN_KEY); пустое значение
выключает управление, как у POST /collect/run и POST /debug/event.
Страница консоли GET /admin/ui открыта — данные в ней нечувствительны,
а все действия всё равно требуют ключ.
"""

from __future__ import annotations

import hmac
import time
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel, Field

from .state import admin_key, state

templates = Jinja2Templates(directory=str(Path(__file__).parent / "templates"))
router = APIRouter(prefix="/admin")


def require_admin(request: Request) -> None:
    key = admin_key()
    if not key:
        raise HTTPException(status_code=404, detail="admin is disabled (MOCK_ADMIN_KEY is empty)")
    if not hmac.compare_digest(request.headers.get("x-admin-key", ""), key):
        raise HTTPException(status_code=403, detail="invalid X-Admin-Key")


class ModeBody(BaseModel):
    mode: str = Field(pattern="^(batch|stream)$")


class ChaosBody(BaseModel):
    code: int = Field(0)
    every: int = Field(5, ge=1, le=100)


class DelayBody(BaseModel):
    ms: int = Field(0, ge=0, le=5000)


class RateLimitBody(BaseModel):
    per_minute: int = Field(0, ge=0, le=10_000)


class BadDataBody(BaseModel):
    enabled: bool
    rate: float = Field(0.2, ge=0, le=1)


class ResetBody(BaseModel):
    seed: int | None = Field(None, ge=0)


def _snapshot() -> dict:
    from .main import items  # лениво: цикл импорта main → admin

    return {
        "mode": state["mode"],
        "total": len(items),
        "publications": state["publications"],
        "seed": state["seed"],
        "uptime_s": int(time.time() - state["started_at"]),
        "requests": dict(state["requests"]),
        "limited_responses": state["limited_responses"],
        "chaos": {"code": state["chaos_code"], "every": state["chaos_every"]},
        "delay_ms": state["delay_ms"],
        "rate_per_minute": state["rate_per_minute"],
        "bad_data": dict(state["bad_data"]),
    }


@router.get("/state", dependencies=[Depends(require_admin)])
def admin_state() -> dict:
    return _snapshot()


def _respond(request: Request, payload: dict):
    """API — JSON; htmx-кнопкам консоли — обновлённый фрагмент статуса,
    чтобы swap не подменял панель сырым JSON (и не ломал поллинг)."""
    if request.headers.get("HX-Request") == "true":
        return _status_fragment(request)
    return payload


@router.post("/mode", dependencies=[Depends(require_admin)])
def admin_mode(request: Request, body: ModeBody):
    state["mode"] = body.mode
    return _respond(request, {"ok": True, "mode": body.mode})


@router.post("/chaos", dependencies=[Depends(require_admin)])
def admin_chaos(request: Request, body: ChaosBody):
    if body.code not in (0, 429, 500):
        raise HTTPException(status_code=422, detail="code must be 0, 429 or 500")
    state["chaos_code"] = body.code
    state["chaos_every"] = body.every
    state["chaos_counter"] = 0
    return _respond(request, {"ok": True, **_snapshot()["chaos"]})


@router.post("/delay", dependencies=[Depends(require_admin)])
def admin_delay(request: Request, body: DelayBody):
    state["delay_ms"] = body.ms
    return _respond(request, {"ok": True, "delay_ms": body.ms})


@router.post("/rate-limit", dependencies=[Depends(require_admin)])
def admin_rate_limit(request: Request, body: RateLimitBody):
    state["rate_per_minute"] = body.per_minute
    state["rate_window"].clear()
    return _respond(request, {"ok": True, "rate_per_minute": body.per_minute})


@router.post("/bad-data", dependencies=[Depends(require_admin)])
def admin_bad_data(request: Request, body: BadDataBody):
    state["bad_data"] = {"enabled": body.enabled, "rate": body.rate}
    return _respond(request, {"ok": True, "bad_data": dict(state["bad_data"])})


@router.post("/reset", dependencies=[Depends(require_admin)])
def admin_reset(request: Request, body: ResetBody):
    from .main import reset_items

    reset_items(body.seed if body.seed is not None else state["seed"])
    return _respond(request, {"ok": True, "seed": state["seed"], "total": _snapshot()["total"]})


def _fmt_uptime(seconds: int) -> str:
    h, rem = divmod(seconds, 3600)
    m, s = divmod(rem, 60)
    return f"{h:02d}:{m:02d}:{s:02d}"


def _status_fragment(request: Request):
    snap = _snapshot()
    return templates.TemplateResponse(
        request,
        "_status.html",
        {
            "s": snap,
            "uptime": _fmt_uptime(snap["uptime_s"]),
            "admin_enabled": bool(admin_key()),
        },
    )


@router.get("/ui/state", response_class=HTMLResponse)
def ui_state(request: Request):
    return _status_fragment(request)


@router.get("/ui/feed", response_class=HTMLResponse)
def ui_feed(request: Request):
    return templates.TemplateResponse(request, "_feed.html", {"feed": state["feed"]})


@router.get("/ui", response_class=HTMLResponse)
def ui(request: Request):
    return templates.TemplateResponse(
        request,
        "admin.html",
        {"admin_enabled": bool(admin_key())},
    )
