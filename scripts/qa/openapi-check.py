#!/usr/bin/env python3
"""OpenAPI-гейт (моя зона CI): OpenAPI-схема core-api не пустеет и не худеет
по мере мержей. Читает JSON со stdin (или --url), проверяет обязательный
набор путей. Незнакомые пути — ок (запрет только на исчезновение).

Обязательный сейчас: каркас health (ФТ-2.7). Когда в main появятся
/api/v1/*, добавить их в REQUIRED (сделаю при приёмке соответствующих PR).
"""
import json
import sys

# Windows-консоли часто не хватает UTF-8 (cp866) — выравниваем явно.
for stream in (sys.stdout, sys.stderr):
    if hasattr(stream, "reconfigure"):
        stream.reconfigure(encoding="utf-8")

REQUIRED = {
    "/health",
    "/health/live",
    "/health/ready",
}
# /openapi.json и /docs — документы схемы, в paths не перечислены.

# Пока не реализованы — предупреждаём, не валим (снимается при мерже ФТ-2.1–2.4).
EXPECTED_SOON = {
    "/api/v1/listings": "ФТ-2.1/2.2",
    "/api/v1/countries": "ФТ-2.4",
    "/api/v1/cities": "ФТ-2.4",
    "/api/v1/sources": "ФТ-2.4",
    "/api/v1/currencies": "ФТ-2.4",
}


def main() -> int:
    data = json.load(sys.stdin)
    paths = set(data.get("paths", {}))
    missing = REQUIRED - paths
    if missing:
        print("FAIL: из OpenAPI исчезли обязательные пути:", ", ".join(sorted(missing)))
        return 1
    soon = [p for p in EXPECTED_SOON if p not in paths]
    if soon:
        print("WARN: ещё не реализованы (ок, но не забыть):",
              ", ".join(f"{p} ({EXPECTED_SOON[p]})" for p in sorted(soon)))
    print(f"OK: обязательные пути на месте ({len(paths)} путей в схеме)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
