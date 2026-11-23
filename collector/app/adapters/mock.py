"""MockAdapter (ФТ-1.1/1.2/1.8): локальные данные вместо внешнего API.

batch — чтение дампа RawListing из файла (MOCK_DUMP_PATH); external_id
берутся из дампа и стабильны между импортами (ФТ-1.8).
stream — fetch_stream_one() возвращает одну синтетическую запись;
интервалы 5–30 с задаёт планировщик (app.runner), не адаптер.

Города и валюты — самостоятельный набор до появления справочников
core-api (ФТ-2.4); при их появлении синхронизировать наборы.
"""

import json
import random
import uuid
from datetime import UTC, datetime
from pathlib import Path

from app.adapters.base import AdapterError, ListingAdapter
from app.schemas import RawListing

# Страна, канонический город, валюта и примерный курс валюты к USD —
# только для правдоподобных цен; пересчёт всё равно делает нормализация.
_STREAM_COUNTRIES = [
    ("BY", "Minsk", "BYN", 0.33),
    ("RU", "Moscow", "RUB", 0.01),
    ("PL", "Warsaw", "PLN", 0.25),
    ("UA", "Kyiv", "UAH", 0.024),
    ("KZ", "Almaty", "KZT", 0.002),
]
# Иногда пишем город локальным именем: нормализация канонизирует по
# справочнику, когда он появится; без справочников запись пропускается
# с причиной — это видно в логах и реестре запусков.
_CITY_ALIASES = {"Minsk": "Минск", "Moscow": "Москва", "Warsaw": "Варшава"}
_STREETS = ["Притыцкого", "Независимости", "Садовая", "Мира", "Гагарина"]
_DESCRIPTIONS = [
    "Светлая квартира после ремонта",
    "Рядом школа и детский сад",
    "Тихий район, зелёный двор",
    "Балкон с видом на парк",
]
_HTML_WRAPS = ["<b>{}</b>", "<i>{}</i>", "{}"]


class MockAdapter(ListingAdapter):
    code = "mock"

    def __init__(
        self,
        dump_path: str | Path,
        rng: random.Random | None = None,
    ) -> None:
        self._dump_path = Path(dump_path)
        self._rng = rng or random.Random()

    async def fetch(self) -> list[RawListing]:
        """Весь дамп (batch); ID стабильны, записи — RawListing."""
        try:
            text = self._dump_path.read_text(encoding="utf-8")
        except OSError as exc:
            raise AdapterError(f"дамп недоступен ({self._dump_path}): {exc}") from exc
        try:
            data = json.loads(text)
        except ValueError as exc:
            raise AdapterError(f"дамп повреждён: {exc}") from exc
        if not isinstance(data, list):
            raise AdapterError("дамп должен быть JSON-массивом")
        try:
            return [RawListing.model_validate(item) for item in data]
        except ValueError as exc:  # ValidationError наследует ValueError
            raise AdapterError(
                f"запись дампа не соответствует RawListing: {exc}"
            ) from exc

    async def fetch_stream_one(self) -> RawListing:
        """Одна новая запись (ФТ-1.8): external_id с префиксом gen- не
        пересекается с ID дампа (seed-) и уникален."""
        country, city, currency, usd_rate = self._rng.choice(_STREAM_COUNTRIES)
        if self._rng.random() < 0.7:
            currency, usd_rate = "USD", 1.0
        rooms = self._rng.choice([0, 1, 2, 3])
        area = round(self._rng.uniform(20, 110), 1)
        price = int(self._rng.uniform(100, 1500) / usd_rate)
        external_id = f"gen-{uuid.uuid4().hex[:12]}"
        if city in _CITY_ALIASES and self._rng.random() < 0.15:
            city = _CITY_ALIASES[city]
        description = self._rng.choice(_HTML_WRAPS).format(
            ", ".join(self._rng.sample(_DESCRIPTIONS, 2))
        )
        rooms_word = f"{rooms}-к квартира" if rooms > 0 else "Студия"
        return RawListing(
            source="mock",
            external_id=external_id,
            deal_type=self._rng.choice(["rent", "sale"]),
            country=country,
            city=city,
            rooms="студия" if rooms == 0 else rooms,
            area_m2=area,
            price=price,
            currency=currency,
            url=f"http://mock.example/listing/{external_id}",
            title=f"{rooms_word}, {area} м²",
            address=f"ул. {self._rng.choice(_STREETS)}, {self._rng.randint(1, 99)}",
            description=description,
            floor=self._rng.randint(1, 9),
            published_at=datetime.now(UTC).isoformat(timespec="seconds"),
        )
