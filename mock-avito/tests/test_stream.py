"""Stream-режим: новые объявления каждые 5–30 с с уникальными ID.

Порядок тестов важен: этот файл перезагружает app.main с
MOCK_PUBLISH_MODE=stream, поэтому идёт последним (после test_app и test_chaos).
"""

import importlib
import time

import app.generator as generator_mod
import app.main as main_mod
import pytest
from fastapi.testclient import TestClient

# Модульный TestClient здесь не нужен: fixture stream_app создаёт свой
# клиент на перезагруженном app (TestClient(reloaded.app)).


@pytest.fixture(scope="module", autouse=True)
def stream_app():
    # Перезагружаем модуль в stream-режиме с ускоренным интервалом публикации.
    import os

    from app.state import state as st

    os.environ["MOCK_PUBLISH_MODE"] = "stream"
    os.environ["MOCK_STREAM_MIN_SEC"] = "0.01"
    os.environ["MOCK_STREAM_MAX_SEC"] = "0.03"
    reloaded = importlib.reload(main_mod)
    st["mode"] = "stream"  # mode живёт в state-словаре, reload его не меняет
    with TestClient(reloaded.app) as c:  # контекст запускает фоновую публикацию
        yield c
    st["mode"] = "batch"
    os.environ["MOCK_PUBLISH_MODE"] = "batch"
    os.environ.pop("MOCK_STREAM_MIN_SEC", None)
    os.environ.pop("MOCK_STREAM_MAX_SEC", None)


def test_mode_is_stream(stream_app):
    body = stream_app.get("/health").json()
    assert body["mode"] == "stream"


def test_new_items_appear(stream_app):
    before = stream_app.get("/health").json()["total"]
    time.sleep(0.3)  # при интервале 0.01–0.03 с должно прийти ~10–30 новых
    after = stream_app.get("/health").json()["total"]
    assert after > before


def test_fresh_item_sorts_first(stream_app):
    # У gen-объявления published_at = момент публикации — оно самое свежее,
    # значит в сортировке published_at desc встаёт первым.
    stream_app.get("/search/listings", params={"size": 1})
    time.sleep(0.1)
    top = stream_app.get("/search/listings", params={"size": 1}).json()["items"][0]
    assert top["id"].startswith("gen-")


def test_stream_ids_unique_and_valid(stream_app):
    page = stream_app.get("/search/listings", params={"size": 100, "page": 1}).json()
    ids = [it["id"] for it in page["items"] if it["id"].startswith("gen-")]
    assert ids and len(ids) == len(set(ids))
    assert all(i["url"].endswith(i["id"]) for i in page["items"])


def test_stream_item_reusable_generator(stream_app):
    rng = __import__("random").Random(1)
    a = generator_mod.generate_stream_item(1, rng)
    b = generator_mod.generate_stream_item(2, rng)
    assert a["id"] == "gen-000001" and b["id"] == "gen-000002"
    assert a["id"] != b["id"]
