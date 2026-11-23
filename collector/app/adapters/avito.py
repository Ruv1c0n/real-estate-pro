"""AvitoApiAdapter (ФТ-1.1): внешний API источника → RawListing.

Схема ответа источника (контракт адаптера, contracts.md §3) — совместима
с mock-avito (PR #62), который на стенде заменяет реальный API Авито:

    GET {base_url}/search/listings?page=N&size=K
    Authorization: Bearer <SOURCE_API_TOKEN>
    → 200 {"items": [...], "total": int, "page": int, "size": int}

    item: {
      "id": str, "deal_type": "rent"|"sale", "title": str,
      "price": int|float, "currency": str, "country": str,
      "city": str, "address": str|null, "rooms": int,
      "area": float, "floor": int, "description": str,
      "images": [str], "published_at": str, "url": str,
      "seller": {...}   # поле источника, в RawListing не входит
    }

Пагинация скрыта: fetch() идёт страницами, пока страница не станет
неполной или не закончится лимит. 429/5xx/сеть → AdapterError — раннер
применит fallback на mock (ФТ-1.2).
"""

import httpx

from app.adapters.base import AdapterError, ListingAdapter, SourceConfigError
from app.schemas import RawListing


class AvitoApiAdapter(ListingAdapter):
    code = "avito"

    def __init__(
        self,
        base_url: str,
        token: str,
        client: httpx.AsyncClient,
        page_size: int = 50,
        max_pages: int = 20,
    ) -> None:
        if not base_url or not token:
            raise SourceConfigError("SOURCE_API_URL/SOURCE_API_TOKEN не заданы")
        self._base_url = base_url.rstrip("/")
        self._token = token
        self._client = client
        self._page_size = page_size
        self._max_pages = max_pages

    async def fetch(self) -> list[RawListing]:
        """Все доступные записи источника; пагинация скрыта (ФТ-1.1)."""
        result: list[RawListing] = []
        for page in range(1, self._max_pages + 1):
            items = await self._fetch_page(page)
            result.extend(items)
            if len(items) < self._page_size:
                break
        return result

    async def _fetch_page(self, page: int) -> list[RawListing]:
        try:
            response = await self._client.get(
                f"{self._base_url}/search/listings",
                params={"page": page, "size": self._page_size},
                headers={"Authorization": f"Bearer {self._token}"},
            )
            response.raise_for_status()
            body = response.json()
        except (httpx.HTTPError, ValueError) as exc:
            raise AdapterError(f"источник недоступен: {exc}") from exc
        items = body.get("items") if isinstance(body, dict) else None
        if not isinstance(items, list):
            raise AdapterError("неожиданный формат ответа: нет массива items")
        raws: list[RawListing] = []
        for item in items:
            try:
                raws.append(self._to_raw(item))
            except (KeyError, TypeError) as exc:
                raise AdapterError(f"запись без обязательного поля: {exc}") from exc
        return raws

    @staticmethod
    def _to_raw(item: dict) -> RawListing:
        return RawListing(
            source="avito",
            external_id=str(item["id"]),
            deal_type=item.get("deal_type"),
            country=item.get("country"),
            city=item.get("city"),
            rooms=item.get("rooms"),
            area_m2=item.get("area"),
            price=item.get("price"),
            currency=item.get("currency"),
            url=item.get("url"),
            title=item.get("title"),
            address=item.get("address"),
            description=item.get("description"),
            floor=item.get("floor"),
            photos=item.get("images"),
            published_at=item.get("published_at"),
        )
