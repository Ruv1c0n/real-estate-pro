"""Изменяемое на лету состояние mock-avito (управляется через /admin).

ADMIN_KEY читается на каждый запрос (не при импорте), чтобы тесты могли
задавать его через monkeypatch.setenv. Всё изменяемое живёт в одном словаре
`state`; вспомогательные структуры — deque-и внутри него.
"""

from __future__ import annotations

import os
import time
from collections import deque


def admin_key() -> str:
    return os.getenv("MOCK_ADMIN_KEY", "")


state: dict = {
    "mode": os.getenv("MOCK_PUBLISH_MODE", "batch").lower(),
    "seed": int(os.getenv("MOCK_SEED", "42")),
    # Глобальный хаос: каждый N-й запрос к /search/listings возвращает код.
    "chaos_code": 0,  # 0 = выключен
    "chaos_every": 5,
    "chaos_counter": 0,
    # Искусственная задержка ответов, мс (тест таймаутов collector'а).
    "delay_ms": 0,
    # Глобальный rate limit на /search/listings и /item; 0 = выключен.
    "rate_per_minute": 0,
    # Подмешивание ломаных объявлений — проверка 422-ветки Collector → Core API.
    "bad_data": {"enabled": False, "rate": 0.2},
    # Метрики.
    "started_at": time.time(),
    "publications": 0,
    "requests": {"search": 0, "item": 0, "admin": 0},
    "limited_responses": 0,
    # Серии per-request хаоса (?fail=429&fail_times=N): оставшиеся сбои по коду.
    "chaos_left": {},
    # Окно rate limit: моменты времени последних запросов к лимитируемым ручкам.
    "rate_window": deque(maxlen=50_000),
    # Лента последних публикаций для консоли (свежие первыми).
    "feed": deque(maxlen=20),
}
