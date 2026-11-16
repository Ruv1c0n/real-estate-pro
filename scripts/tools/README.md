# scripts/tools — инструменты-заделы

## sql-to-er.py — ER-диаграмма из SQL-миграций (Mermaid)

Генерирует Mermaid `erDiagram` из SQL-файлов миграций. Stdlib-only,
зависимостей не требует. Задел под ТЗ 6.1 («ER-диаграмма» — обязанность
Ильи): инструмент снимает ручную синхронизацию диаграммы со схемой.

```bash
# notifier-миграции сейчас; core-миграции подхватятся, когда появятся
python -I scripts/tools/sql-to-er.py notifier/migrations/*.sql -o docs/er-notifier.mmd
python -I scripts/tools/sql-to-er.py --dir notifier/migrations   # каталог
python -I scripts/tools/sql-to-er.py notifier/migrations/*.sql   # stdout
```

Что покрывается:

- `CREATE TABLE [IF NOT EXISTS] [схема.]имя (...)` — колонки с типом,
  inline `PRIMARY KEY` / `UNIQUE` / `NOT NULL`, inline `REFERENCES` (FK),
  табличные `CONSTRAINT ... PRIMARY KEY / UNIQUE / FOREIGN KEY`;
- `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY (col) REFERENCES ...`;
- `ALTER TABLE ... ADD [COLUMN] [IF NOT EXISTS] имя тип ...` (в т.ч. с FK);
- `CHECK` / `DEFAULT` аккуратно пропускаются (учитываются скобки и строки
  в кавычках — например, regex в CHECK не ломает разбор).

Что НЕ покрывается (warning в stderr, не падение): `CREATE TABLE AS/LIKE`,
`PARTITION BY`, наследование `INHERITS`, `EXCLUDE`, `CREATE INDEX` (молча
игнорируется). Ссылка FK на таблицу, у которой нет `CREATE TABLE` в поданных
файлах, даёт «стаб»-сущность с warning. Имена сущностей — без префикса схемы
(`notifier.users` → `users`), типы санитизируются под синтаксис Mermaid
(`NUMERIC(10,2)` → `NUMERIC`).

Парсер ограниченный: если миграция использует экзотический синтаксис,
проверяйте stderr и итоговую диаграмму глазами. Рендерить Mermaid можно,
например, через встроенную поддержку GitHub / mermaid.live.
