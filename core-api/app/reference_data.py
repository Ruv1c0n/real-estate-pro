from decimal import Decimal
from typing import Annotated

import simplejson
from fastapi import APIRouter, Query, Request, Response
from pydantic import BaseModel, WithJsonSchema
from sqlalchemy import text

router = APIRouter(prefix="/api/v1", tags=["reference data"])


class NamedEntry(BaseModel):
    code: str
    name: str


class City(BaseModel):
    country: str
    name: str
    aliases: list[str]


class Currency(BaseModel):
    code: str
    rate_to_usd: Annotated[
        Decimal, WithJsonSchema({"type": "number"}, mode="serialization")
    ]


async def _rows(
    request: Request, sql: str, **params: str | None
) -> list[dict[str, object]]:
    async with request.app.state.db_engine.connect() as connection:
        result = await connection.execute(text(sql), params)
        return [dict(row) for row in result.mappings()]


@router.get("/countries", response_model=list[NamedEntry])
async def countries(request: Request):
    return await _rows(request, "SELECT code, name FROM core.countries ORDER BY code")


@router.get("/sources", response_model=list[NamedEntry])
async def sources(request: Request):
    return await _rows(request, "SELECT code, name FROM core.sources ORDER BY code")


@router.get("/cities", response_model=list[City])
async def cities(
    request: Request,
    country: Annotated[
        str | None, Query(min_length=2, max_length=2, pattern=r"^[A-Z]{2}$")
    ] = None,
):
    return await _rows(
        request,
        """
        SELECT c.country_code AS country, c.name,
               ARRAY(SELECT a.alias FROM core.city_name_aliases AS a
                     WHERE a.city_id = c.id ORDER BY a.alias) AS aliases
        FROM core.cities AS c
        WHERE CAST(:country AS text) IS NULL OR c.country_code = :country
        ORDER BY c.country_code, c.name
        """,
        country=country,
    )


@router.get("/currencies", response_model=list[Currency])
async def currencies(request: Request) -> Response:
    rows = await _rows(
        request, "SELECT code, rate_to_usd FROM core.currencies ORDER BY code"
    )
    return Response(
        content=simplejson.dumps(rows, use_decimal=True), media_type="application/json"
    )
