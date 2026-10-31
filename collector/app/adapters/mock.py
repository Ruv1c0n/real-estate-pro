from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Any

from .base import SourceAdapter

log = logging.getLogger(__name__)


class MockAdapter(SourceAdapter):
    name = "mock"

    def __init__(self, path: str):
        self.path = Path(path)

    def fetch(self) -> list[dict[str, Any]]:
        with self.path.open(encoding="utf-8") as f:
            items = json.load(f)
        log.info("mock: загружено %d записей из %s", len(items), self.path)
        return items
