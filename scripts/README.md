# scripts

Вспомогательные скрипты разработки и эксплуатации. Всё опционально: сервисы работают без них.

| Путь | Назначение |
|---|---|
| [trail-viz/gen.py](trail-viz/gen.py) | Детерминированная кластерная раскладка trail-графа (узлы/рёбра knowledge-graph) для GitHub Pages; рендер проверяется headless-браузером. |
| [db/backup.sh](db/backup.sh), [db/restore-verify.sh](db/restore-verify.sh) | Backup/restore-verify PostgreSQL (регламент «дамп без успешной restore-verify бэкапом не считается», PR #83; регламент — [docs/db-backup-restore.md](../docs/db-backup-restore.md)) |

## Планируется (набросок)

- `scripts/tools/` — `sql-to-er.py` (ER-диаграмма из SQL, в т.ч. составные FK) и каркас бенчмарка НФТ-10: идут в PR #76.
- `scripts/db/gen-analysis-data.py` — генератор демо-данных для анализа запросов схемы core: придёт с переносом стека (PR #92).

После мержа таблица дополняется фактическими командами запуска.
