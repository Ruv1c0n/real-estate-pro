"""Redis-дедуп (ФТ-1.5): быстрая проверка до отправки; окончательная
уникальность обеспечивают Core API и PostgreSQL. Хэш помечается
обработанным только после успешного ответа Core API (200/201).

При недоступности Redis отправка продолжается через идемпотентный API
(НФТ-7): processed() отвечает «не видели», mark_processed() молчит;
смена состояния логируется один раз, без спама на каждой записи.
"""

import hashlib
import logging

logger = logging.getLogger(__name__)

_KEY_PREFIX = "collector:processed:"


class RedisDedup:
    def __init__(self, redis_client) -> None:
        # redis.asyncio.Redis; используются только exists/set, поэтому
        # в тестах подставляется любой объект с теми же методами.
        self._redis = redis_client
        self._down = False

    @staticmethod
    def _key(source: str, external_id: str) -> str:
        digest = hashlib.sha256(f"{source}:{external_id}".encode()).hexdigest()
        return f"{_KEY_PREFIX}{digest}"

    async def processed(self, source: str, external_id: str) -> bool:
        try:
            exists = await self._redis.exists(self._key(source, external_id))
        except Exception as exc:  # любая ошибка Redis = деградация, не падение
            self._log_state(down=True, exc=exc)
            return False
        self._log_state(down=False)
        return bool(exists)

    async def mark_processed(self, source: str, external_id: str) -> None:
        try:
            await self._redis.set(self._key(source, external_id), "1")
        except Exception as exc:  # см. processed()
            self._log_state(down=True, exc=exc)

    def _log_state(self, down: bool, exc: Exception | None = None) -> None:
        if down and not self._down:
            logger.warning("Redis недоступен — дедуп пропускается: %s", exc)
        elif not down and self._down:
            logger.info("Redis снова доступен — дедуп работает")
        self._down = down
