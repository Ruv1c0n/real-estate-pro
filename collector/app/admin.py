"""POST /collect/run и GET /collect/runs/{run_id} (ФТ-1.4, НФТ-3).

Доступ с заголовком X-Admin-Key; ключ задаётся конфигурацией (ADMIN_KEY).
Пустой ключ открывает эндпоинты — dev-режим, предупреждение логируется
при старте (то же соглашение, что у /debug/event в notifier).
"""

from typing import Annotated

from fastapi import APIRouter, Depends, Header, HTTPException, Request, status
from pydantic import BaseModel

from app.runner import RunInfo, RunInProgressError


async def require_admin_key(
    request: Request,
    x_admin_key: Annotated[str | None, Header(alias="X-Admin-Key")] = None,
) -> None:
    settings = request.app.state.settings
    if settings.admin_key and x_admin_key != settings.admin_key:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Требуется корректный заголовок X-Admin-Key",
        )


router = APIRouter(tags=["collect"], dependencies=[Depends(require_admin_key)])


class RunAccepted(BaseModel):
    run_id: str


@router.post("/collect/run", status_code=status.HTTP_202_ACCEPTED)
async def start_collect_run(request: Request) -> RunAccepted:
    """Ставит сбор в фон и возвращает run_id; активный запуск — 409."""
    runner = request.app.state.runner
    try:
        run_id = await runner.start_cycle("manual")
    except RunInProgressError as exc:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT, detail=str(exc)
        ) from None
    return RunAccepted(run_id=run_id)


@router.get("/collect/runs/{run_id}")
async def collect_run_status(run_id: str, request: Request) -> RunInfo:
    info = request.app.state.runner.run_info(run_id)
    if info is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Запуск не найден"
        )
    return info
