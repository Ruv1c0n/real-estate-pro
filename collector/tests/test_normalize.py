import json
from pathlib import Path

import pytest

from app.config import Settings
from app.normalize import NormalizeError, normalize, parse_rooms

CFG = Settings(mock_mode=True)
DATA = Path(__file__).resolve().parent.parent / "data" / "listings.json"


def test_rooms():
    assert parse_rooms("Студия") == 0
    assert parse_rooms("2-комн.") == 2
    assert parse_rooms("3 rooms") == 3
    assert parse_rooms(1) == 1


def test_all_mock_items_normalize_except_broken():
    items = json.loads(DATA.read_text(encoding="utf-8"))
    ok, bad = 0, 0
    hashes = set()
    for raw in items:
        try:
            lst = normalize(raw, CFG)
            ok += 1
            hashes.add(lst.url_hash)
            assert "<" not in lst.description
            assert lst.price_usd > 0
        except NormalizeError:
            bad += 1
    assert ok >= 500
    assert bad == 2
    assert len(hashes) == ok


def test_url_hash_ignores_query():
    raw = {"source": "mock", "id": "1", "link": "https://a/b?x=1", "title": "t", "deal": "rent",
           "price": 100, "currency": "USD", "country": "BY", "city": "Минск", "rooms": 1}
    a = normalize(raw, CFG)
    raw["link"] = "https://a/b?x=2"
    assert a.url_hash == normalize(raw, CFG).url_hash


def test_invalid_deal():
    with pytest.raises(NormalizeError):
        normalize({"source": "m", "id": "1", "link": "u", "title": "t", "deal": "обмен",
                   "price": 1, "currency": "USD", "country": "BY", "city": "x", "rooms": 1}, CFG)
