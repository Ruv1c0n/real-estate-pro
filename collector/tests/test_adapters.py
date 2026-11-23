"""Адаптеры (ФТ-1.1/1.2/1.8): mock batch/stream и AvitoApiAdapter."""

import json
import random

import httpx
import pytest
from conftest import make_raw

from app.adapters.avito import AvitoApiAdapter
from app.adapters.base import AdapterError, SourceConfigError
from app.adapters.mock import MockAdapter


def write(tmp_path, records) -> object:
    path = tmp_path / "dump.json"
    path.write_text(json.dumps(records, ensure_ascii=False), encoding="utf-8")
    return path


@pytest.mark.anyio
async def test_mock_batch_ids_stable(tmp_path):
    path = write(tmp_path, [make_raw("seed-1"), make_raw("seed-2")])
    adapter = MockAdapter(path, rng=random.Random(1))

    first = await adapter.fetch()
    second = await adapter.fetch()

    assert [r.external_id for r in first] == ["seed-1", "seed-2"]
    assert [r.external_id for r in second] == ["seed-1", "seed-2"]  # стабильные ID


@pytest.mark.anyio
async def test_mock_dump_missing(tmp_path):
    adapter = MockAdapter(tmp_path / "absent.json")
    with pytest.raises(AdapterError):
        await adapter.fetch()


@pytest.mark.anyio
async def test_mock_dump_corrupted(tmp_path):
    path = tmp_path / "dump.json"
    path.write_text("{not json", encoding="utf-8")
    with pytest.raises(AdapterError):
        await MockAdapter(path).fetch()


@pytest.mark.anyio
async def test_stream_ids_unique_and_disjoint_from_dump(tmp_path):
    path = write(tmp_path, [make_raw("seed-000001")])
    adapter = MockAdapter(path, rng=random.Random(2))

    ids = {(await adapter.fetch_stream_one()).external_id for _ in range(30)}

    assert len(ids) == 30  # уникальные
    assert all(i.startswith("gen-") for i in ids)  # не пересекаются с seed-


@pytest.mark.anyio
async def test_stream_rooms_studio_or_int(tmp_path):
    adapter = MockAdapter(tmp_path / "unused.json", rng=random.Random(3))
    values = {(await adapter.fetch_stream_one()).rooms for _ in range(50)}
    assert values <= {1, 2, 3, "студия"}


def test_avito_requires_credentials():
    with pytest.raises(SourceConfigError):
        AvitoApiAdapter("", "", httpx.AsyncClient())


@pytest.mark.anyio
async def test_avito_hides_pagination():
    # Формат ответа источника: items с "id" (см. AvitoApiAdapter docstring)
    source_items = [{"id": f"seed-{i}", "deal_type": "rent"} for i in range(1, 4)]
    seen_pages: list[int] = []

    def handler(request: httpx.Request) -> httpx.Response:
        page = int(request.url.params["page"])
        seen_pages.append(page)
        items = source_items[:2] if page == 1 else source_items[2:]
        return httpx.Response(
            200,
            json={"items": items, "total": len(source_items), "page": page, "size": 2},
        )

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    adapter = AvitoApiAdapter(
        "http://source.test", "token", client, page_size=2, max_pages=5
    )
    raws = await adapter.fetch()
    await client.aclose()

    assert seen_pages == [1, 2]  # страница 2 неполная — стоп, наружу один fetch()
    assert len(raws) == 3


@pytest.mark.anyio
async def test_avito_maps_source_fields():
    item = {
        "id": "seed-000042",
        "deal_type": "sale",
        "title": "<b>3-к</b>, 70 м²",
        "price": 95000,
        "currency": "USD",
        "country": "BY",
        "city": "Minsk",
        "address": "Минск, ул. Мира, 5",
        "rooms": 3,
        "area": 70.5,
        "floor": 4,
        "description": "описание",
        "images": ["http://mock-avito:8002/img/1.jpg"],
        "published_at": "2026-11-18T12:00:00+03:00",
        "url": "http://mock-avito:8002/item/seed-000042",
        "seller": {"name": "Иван", "rating": 4.9},
    }

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, json={"items": [item], "total": 1, "page": 1, "size": 50}
        )

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    adapter = AvitoApiAdapter("http://source.test", "token", client, page_size=50)
    raws = await adapter.fetch()
    await client.aclose()

    raw = raws[0]
    assert raw.external_id == "seed-000042"
    assert raw.source == "avito"
    assert raw.area_m2 == 70.5  # поле источника area → area_m2
    assert raw.photos == ["http://mock-avito:8002/img/1.jpg"]  # images → photos
    assert "seller" not in raw.model_dump()  # поле источника не входит в RawListing


@pytest.mark.anyio
async def test_avito_500_raises_adapter_error():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, json={"error": "internal_error"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    adapter = AvitoApiAdapter("http://source.test", "token", client)
    with pytest.raises(AdapterError):
        await adapter.fetch()
    await client.aclose()
