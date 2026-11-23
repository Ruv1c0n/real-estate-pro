"""Нормализация RawListing → ListingPayload (ФТ-1.3).

Правила (ТЗ раздел 3, contracts.md «Общие соглашения»):
- price_usd = price × rate_to_usd, Decimal, округление ROUND_HALF_UP
  до целого USD (USD=1 задан контрактом и справочников не требует);
- комнаты — целое ≥0, «студия» = 0;
- площадь >0, ≤2 знаков после запятой (лимит core-api);
- title/description очищаются от HTML;
- страна/город/валюта/источник — по справочникам Core API (ФТ-2.4).
  Пока справочники недоступны (ФТ-2.4 не смержен), работает промежуточный
  режим: коды проверяются по формату, канонизация городов и пересчёт
  не-USD валют невозможны — такие записи пропускаются с причиной.

Неполные и некорректные записи пропускаются: сообщение NormalizeError —
причина для лога и реестра запусков.
"""

import logging
import re
from datetime import UTC, datetime
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation
from html.parser import HTMLParser

from app.dicts import DictsSnapshot
from app.schemas import ListingPayload, RawListing

logger = logging.getLogger(__name__)

_COUNTRY_RE = re.compile(r"^[A-Z]{2}$")
_CURRENCY_RE = re.compile(r"^[A-Z]{3}$")
_STUDIO_WORDS = ("студия", "studio")
_AREA_MAX = Decimal("999999.99")  # max_digits=8, decimal_places=2 в core-api
_PRICE_MAX = Decimal("999999999999999.99")  # max_digits=18, decimal_places=2


class NormalizeError(ValueError):
    """Запись некорректна; сообщение — причина пропуска (ФТ-1.3)."""


class _TextExtractor(HTMLParser):
    """Собирает текст HTML-фрагмента, отбрасывая теги."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self._chunks: list[str] = []

    def handle_data(self, data: str) -> None:
        self._chunks.append(data)

    def text(self) -> str:
        return " ".join("".join(self._chunks).split())


def strip_html(text: str | None) -> str | None:
    """Убирает HTML-разметку и сжимает пробелы; None или пусто → None."""
    if text is None:
        return None
    stripped = text.strip()
    if not stripped:
        return None
    if "<" in stripped or "&" in stripped:
        extractor = _TextExtractor()
        try:
            extractor.feed(stripped)
            cleaned = extractor.text()
        except Exception:  # битый HTML не повод терять запись
            cleaned = " ".join(re.sub(r"<[^>]+>", " ", stripped).split())
    else:
        cleaned = " ".join(stripped.split())
    return cleaned or None


def _to_decimal(raw: object, field: str) -> Decimal:
    try:
        value = Decimal(str(raw))
    except (InvalidOperation, ValueError):
        raise NormalizeError(f"{field}: не число ({raw!r})") from None
    if not value.is_finite():
        raise NormalizeError(f"{field}: не конечное число")
    return value


def _parse_rooms(raw: object) -> int:
    if raw is None:
        raise NormalizeError("rooms: значение отсутствует")
    text = str(raw).strip().casefold()
    if text in _STUDIO_WORDS:
        return 0
    try:
        rooms = int(text)
    except ValueError:
        raise NormalizeError(f"rooms: не целое число ({raw!r})") from None
    if rooms < 0:
        raise NormalizeError(f"rooms: отрицательное значение ({rooms})")
    return rooms


def _parse_area(raw: object) -> Decimal | None:
    if raw is None or str(raw).strip() == "":
        return None
    area = _to_decimal(raw, "area_m2")
    if area <= 0:
        raise NormalizeError(f"area_m2: должно быть >0 ({area})")
    area = area.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
    if area > _AREA_MAX:
        raise NormalizeError(f"area_m2: слишком большое ({area})")
    return area


def _parse_published_at(raw: str | None) -> str | None:
    if raw is None or not raw.strip():
        return None
    try:
        moment = datetime.fromisoformat(raw.strip())
    except ValueError:
        raise NormalizeError(f"published_at: не ISO 8601 ({raw!r})") from None
    if moment.tzinfo is None:
        moment = moment.replace(tzinfo=UTC)
    return moment.astimezone(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


def _require_code(raw: str | None, field: str, pattern: re.Pattern[str]) -> str:
    if raw is None or not raw.strip():
        raise NormalizeError(f"{field}: значение отсутствует")
    value = raw.strip().upper()
    if not pattern.fullmatch(value):
        raise NormalizeError(f"{field}: неверный формат ({raw!r})")
    return value


def _currency_rate(currency: str, dicts: DictsSnapshot | None) -> Decimal:
    if currency == "USD":
        return Decimal(1)
    if dicts is None:
        raise NormalizeError(
            f"{currency}: курсы недоступны — справочников Core API нет (ФТ-2.4)"
        )
    rate = dicts.currencies.get(currency)
    if rate is None or rate <= 0:
        raise NormalizeError(f"currency: нет в справочнике ({currency})")
    return rate


def _parse_photos(raw_photos: list[str] | None) -> list[str] | None:
    if not raw_photos:
        return None
    photos: list[str] = []
    for photo in raw_photos:
        cleaned = photo.strip()
        if cleaned.lower().startswith(("http://", "https://")):
            photos.append(cleaned)
        else:
            logger.debug("Фото отброшено (не HTTP(S)-ссылка): %r", photo)
    return photos or None


def normalize(raw: RawListing, dicts: DictsSnapshot | None) -> ListingPayload:
    """Приводит RawListing к payload доставки или бросает NormalizeError."""
    source = (raw.source or "").strip().lower()
    external_id = (raw.external_id or "").strip()
    if not source:
        raise NormalizeError("source: значение отсутствует")
    if not external_id:
        raise NormalizeError("external_id: значение отсутствует")
    if dicts is not None and source not in dicts.sources:
        raise NormalizeError(f"source: нет в справочнике ({source})")

    deal_type = (raw.deal_type or "").strip().casefold()
    if deal_type not in ("rent", "sale"):
        raise NormalizeError(f"deal_type: не rent/sale ({raw.deal_type!r})")

    url = (raw.url or "").strip()
    if not url.lower().startswith(("http://", "https://")):
        raise NormalizeError(f"url: не абсолютная HTTP(S)-ссылка ({raw.url!r})")

    price = _to_decimal(raw.price, "price")
    if price <= 0:
        raise NormalizeError(f"price: должно быть >0 ({price})")
    price = price.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
    if price > _PRICE_MAX:
        raise NormalizeError(f"price: слишком большое ({price})")

    currency = _require_code(raw.currency, "currency", _CURRENCY_RE)
    rate = _currency_rate(currency, dicts)
    price_usd = int((price * rate).quantize(Decimal(1), rounding=ROUND_HALF_UP))
    if price_usd <= 0:
        raise NormalizeError(
            f"price_usd: расчёт дал неположительное значение ({price} {currency})"
        )

    country = _require_code(raw.country, "country", _COUNTRY_RE)
    city = (raw.city or "").strip()
    if not city:
        raise NormalizeError("city: значение отсутствует")
    if dicts is not None:
        if country not in dicts.countries:
            raise NormalizeError(f"country: нет в справочнике ({country})")
        canonical = dicts.canonical_city(country, city)
        if canonical is None:
            raise NormalizeError(f"city: нет в справочнике ({city!r} в {country})")
        city = canonical

    floor: int | None = None
    if raw.floor is not None and str(raw.floor).strip() != "":
        try:
            floor = int(str(raw.floor).strip())
        except ValueError:
            raise NormalizeError(f"floor: не целое число ({raw.floor!r})") from None

    return ListingPayload(
        source=source,
        external_id=external_id,
        deal_type=deal_type,
        country=country,
        city=city,
        rooms=_parse_rooms(raw.rooms),
        price=price,
        currency=currency,
        price_usd=price_usd,
        url=url,
        title=strip_html(raw.title),
        address=strip_html(raw.address),
        description=strip_html(raw.description),
        area_m2=_parse_area(raw.area_m2),
        floor=floor,
        photos=_parse_photos(raw.photos),
        published_at=_parse_published_at(raw.published_at),
    )
