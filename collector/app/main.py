from __future__ import annotations

import logging
import time

from .adapters import BlockedError, CianAdapter, MockAdapter, SourceAdapter
from .config import Settings, settings
from .normalize import NormalizeError, normalize
from .publisher import CoreApiPublisher

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
log = logging.getLogger("collector")


def build_adapters(cfg: Settings) -> list[SourceAdapter]:
    if cfg.mock_mode:
        return [MockAdapter(cfg.mock_data_path)]
    registry = {"cian": lambda: CianAdapter(cfg.http_timeout_sec)}
    names = [n.strip() for n in cfg.live_sources.split(",") if n.strip()]
    return [registry[n]() for n in names if n in registry]


def fetch_with_fallback(adapter: SourceAdapter, cfg: Settings) -> list[dict]:
    try:
        items = adapter.fetch()
        if items or not cfg.fallback_to_mock:
            return items
        log.warning("%s вернул 0 записей, переключаемся на mock", adapter.name)
    except BlockedError as e:
        if not cfg.fallback_to_mock:
            raise
        log.warning("%s заблокирован (%s), переключаемся на mock", adapter.name, e)
    return MockAdapter(cfg.mock_data_path).fetch()


def run_cycle(cfg: Settings, publisher: CoreApiPublisher, seen: set[str]) -> dict[str, int]:
    stats = {"created": 0, "duplicate": 0, "error": 0, "invalid": 0, "skipped": 0}
    for adapter in build_adapters(cfg):
        for raw in fetch_with_fallback(adapter, cfg):
            try:
                listing = normalize(raw, cfg)
            except NormalizeError as e:
                stats["invalid"] += 1
                log.warning("пропуск записи %s: %s", raw.get("id"), e)
                continue
            if listing.url_hash in seen:
                stats["skipped"] += 1
                continue
            result = publisher.send(listing)
            stats[result] += 1
            if result in ("created", "duplicate"):
                seen.add(listing.url_hash)
            if cfg.request_delay_sec:
                time.sleep(cfg.request_delay_sec)
    return stats


def main() -> None:
    log.info("collector стартует, mock_mode=%s", settings.mock_mode)
    publisher = CoreApiPublisher(settings.core_api_url, settings.http_timeout_sec)
    publisher.wait_until_ready()
    seen: set[str] = set()
    try:
        while True:
            stats = run_cycle(settings, publisher, seen)
            log.info("цикл завершён: %s", stats)
            time.sleep(settings.collect_interval_sec)
    except KeyboardInterrupt:
        pass
    finally:
        publisher.close()


if __name__ == "__main__":
    main()
