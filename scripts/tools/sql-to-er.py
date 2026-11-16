# -*- coding: utf-8 -*-
"""sql-to-er.py — генератор Mermaid erDiagram из SQL-миграций (stdlib-only).

Задел под ТЗ 6.1 («ER-диаграмма» — обязанность Ильи): парсит SQL-файлы
миграций и рисует сущности с ключевыми колонками и связи по FK.

Покрывается (осознанно ограниченный парсер):
  - CREATE TABLE [IF NOT EXISTS] [схема.]имя ( ... ):
    колонки с типом, inline PRIMARY KEY / UNIQUE / NOT NULL,
    inline REFERENCES [таблица (колонка)] -> FK,
    табличные CONSTRAINT ... PRIMARY KEY / UNIQUE / FOREIGN KEY
    (в т.ч. составные FOREIGN KEY (a, b) REFERENCES p (x, y) — одной
    связью с подписью «a + b (составной FK)»),
    CHECK и DEFAULT пропускаются (тело со скобками и строками аккуратно
    вырезается);
  - ALTER TABLE [ONLY] ... ADD CONSTRAINT ... FOREIGN KEY (col) REFERENCES ...;
  - ALTER TABLE ... ADD [COLUMN] [IF NOT EXISTS] имя тип ... (в т.ч. с REFERENCES).

НЕ покрывается (warning в stderr, не падение): CREATE TABLE AS/LIKE,
PARTITION BY, наследование (INHERITS), EXCLUDE, GENERATED-ссылки на другие
таблицы, EXECUTE/функции, динамический SQL. Ссылка на таблицу, у которой нет
CREATE TABLE в поданных файлах, даёт «стаб»-сущность с warning.

Типы колонок санитизируются под синтаксис Mermaid erDiagram (имена без
скобок/пробелов, например NUMERIC(10,2) -> NUMERIC). Имена сущностей — без
префикса схемы (notifier.users -> users), т.к. точки Mermaid не поддерживает.

Запуск:
  python -I scripts/tools/sql-to-er.py notifier/migrations/*.sql -o docs/er-notifier.mmd
  python -I scripts/tools/sql-to-er.py --dir notifier/migrations -o docs/er-notifier.mmd

Без -o вывод в stdout.
"""
from __future__ import annotations

import argparse
import glob
import os
import re
import sys

# слова, на которых заканчивается «тип» в определении колонки
CONSTRAINT_KEYWORDS = {
    "PRIMARY", "UNIQUE", "NOT", "NULL", "DEFAULT", "REFERENCES",
    "CHECK", "CONSTRAINT", "GENERATED", "COLLATE",
}

TABLE_LEVEL_STARTS = {"PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "EXCLUDE", "CONSTRAINT"}


class Table:
    def __init__(self, name: str) -> None:
        self.name = name            # имя без схемы
        self.columns: list[dict] = []   # {"name","type","pk","uk","fk","not_null"}
        self.by_name: dict[str, dict] = {}

    def col(self, name: str) -> dict | None:
        return self.by_name.get(name.lower())

    def add_column(self, name: str, ctype: str, pk=False, uk=False,
                   fk=False, not_null=False) -> dict:
        existing = self.col(name)
        if existing:
            # повтор (IF NOT EXISTS в другом файле) — только ужесточаем флаги
            existing["pk"] = existing["pk"] or pk
            existing["uk"] = existing["uk"] or uk
            existing["fk"] = existing["fk"] or fk
            return existing
        c = {"name": name, "type": ctype, "pk": pk, "uk": uk,
             "fk": fk, "not_null": not_null}
        self.columns.append(c)
        self.by_name[name.lower()] = c
        return c


class Model:
    def __init__(self) -> None:
        self.tables: dict[str, Table] = {}
        self.order: list[str] = []
        self.fks: list[dict] = []   # {"child","child_col","parent","parent_col"}

    def table(self, name: str) -> Table:
        t = self.tables.get(name)
        if t is None:
            t = Table(name)
            self.tables[name] = t
            self.order.append(name)
        return t


# ---------------------------------------------------------------- лексика SQL

def strip_comments(sql: str) -> str:
    """Убирает -- комментарии и /* */ вне строковых литералов."""
    out: list[str] = []
    i, n = 0, len(sql)
    in_str = False
    while i < n:
        ch = sql[i]
        if in_str:
            out.append(ch)
            if ch == "'":
                # '' — экранированная кавычка внутри строки
                if i + 1 < n and sql[i + 1] == "'":
                    out.append("'")
                    i += 2
                    continue
                in_str = False
            i += 1
            continue
        if ch == "'":
            in_str = True
            out.append(ch)
            i += 1
            continue
        if ch == "-" and i + 1 < n and sql[i + 1] == "-":
            while i < n and sql[i] != "\n":
                i += 1
            continue
        if ch == "/" and i + 1 < n and sql[i + 1] == "*":
            i += 2
            while i + 1 < n and not (sql[i] == "*" and sql[i + 1] == "/"):
                i += 1
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def split_statements(sql: str) -> list[str]:
    """Разбивает на statements по ';' на нулевой глубине скобок, вне строк."""
    stmts: list[str] = []
    buf: list[str] = []
    depth = 0
    in_str = False
    i, n = 0, len(sql)
    while i < n:
        ch = sql[i]
        if in_str:
            buf.append(ch)
            if ch == "'":
                if i + 1 < n and sql[i + 1] == "'":
                    buf.append("'")
                    i += 2
                    continue
                in_str = False
            i += 1
            continue
        if ch == "'":
            in_str = True
            buf.append(ch)
        elif ch == "(":
            depth += 1
            buf.append(ch)
        elif ch == ")":
            depth = max(0, depth - 1)
            buf.append(ch)
        elif ch == ";" and depth == 0:
            text = "".join(buf).strip()
            if text:
                stmts.append(text)
            buf = []
        else:
            buf.append(ch)
        i += 1
    text = "".join(buf).strip()
    if text:
        stmts.append(text)
    return stmts


def split_top_level(body: str) -> list[str]:
    """Разбивает тело CREATE TABLE по ',' на нулевой глубине скобок."""
    items: list[str] = []
    buf: list[str] = []
    depth = 0
    in_str = False
    for i, ch in enumerate(body):
        if in_str:
            buf.append(ch)
            if ch == "'" and not (i + 1 < len(body) and body[i + 1] == "'"):
                in_str = False
            continue
        if ch == "'":
            in_str = True
            buf.append(ch)
        elif ch == "(":
            depth += 1
            buf.append(ch)
        elif ch == ")":
            depth = max(0, depth - 1)
            buf.append(ch)
        elif ch == "," and depth == 0:
            items.append("".join(buf).strip())
            buf = []
        else:
            buf.append(ch)
    if "".join(buf).strip():
        items.append("".join(buf).strip())
    return items


# ------------------------------------------------------------------- парсеры

def entity_name(qualified: str) -> str:
    """notifier.users -> users; снимает двойные кавычки."""
    name = qualified.strip().strip('"')
    if "." in name:
        name = name.rsplit(".", 1)[1]
    return name.strip('"')


def sane_type(ctype: str) -> str:
    """Тип под синтаксис Mermaid: только буквы/цифры/_ (нет скобок и пробелов)."""
    t = re.sub(r"\s*\(.*\)", "", ctype)          # NUMERIC(10,2) -> NUMERIC
    t = re.sub(r"[^0-9A-Za-z_]+", "_", t.strip())
    return t or "TEXT"


def parse_references(item: str) -> tuple[str, str] | None:
    """REFERENCES таблица (колонка) -> (таблица, колонка); без колонки -> ('', '')."""
    m = re.search(
        r"REFERENCES\s+(?:ONLY\s+)?(?:\"?[\w$]+\"?\.)?\"?([\w$]+)\"?"
        r"(?:\s*\(\s*\"?([\w$]+)\"?\s*\))?",
        item, re.IGNORECASE)
    if not m:
        return None
    return m.group(1), m.group(2) or ""


def parse_column(item: str, source: str) -> dict | None:
    """Определение колонки -> {"name","type","pk","uk","fk","not_null","ref"}."""
    m = re.match(r"(?:CONSTRAINT\s+[\w$]+\s+)?\"?([\w$]+)\"?\s+(.+)",
                 item.strip(), re.IGNORECASE | re.DOTALL)
    if not m:
        warn(f"не разобрано определение колонки: {item[:60]!r} ({source})")
        return None
    name = m.group(1)
    rest = m.group(2)
    up = rest.upper()

    # тип: токены до первого слова-ограничителя
    words = rest.split()
    type_words: list[str] = []
    for w in words:
        if w.strip('"').upper() in CONSTRAINT_KEYWORDS:
            break
        type_words.append(w)
    ctype = " ".join(type_words) or "TEXT"

    ref = None
    if "REFERENCES" in up:
        r = parse_references(rest)
        if r:
            ref = r
        else:
            warn(f"REFERENCES не распознан в: {item[:60]!r} ({source})")
    return {
        "name": name,
        "type": sane_type(ctype),
        "pk": bool(re.search(r"\bPRIMARY\s+KEY\b", up)),
        "uk": bool(re.search(r"\bUNIQUE\b", up)),
        "fk": ref is not None,
        "not_null": bool(re.search(r"\bNOT\s+NULL\b", up)),
        "ref": ref,
    }


def parse_create_table(stmt: str, model: Model, source: str) -> None:
    m = re.match(
        r"CREATE\s+(?:UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"
        r"(\"?[\w$]+\"?(?:\.[\w$\".]+)?)\s*\((.*)\)\s*$",
        stmt, re.IGNORECASE | re.DOTALL)
    if not m:
        warn(f"CREATE TABLE не распознан (возможно, AS/LIKE/PARTITION): "
             f"{stmt[:60]!r} ({source})")
        return
    table = model.table(entity_name(m.group(1)))
    body = m.group(2)

    for item in split_top_level(body):
        item_norm = re.sub(r"\s+", " ", item).strip()
        first = item_norm.split(None, 1)[0].strip('"').upper() if item_norm else ""

        # табличные ограничения
        if first == "CONSTRAINT":
            # CONSTRAINT имя CHECK/PRIMARY/UNIQUE/FOREIGN — имя срезаем
            parts = item_norm.split(None, 2)
            if len(parts) < 3:
                warn(f"CONSTRAINT без тела пропущен: {item_norm[:60]!r} ({source})")
                continue
            item_norm = parts[2]
            first = item_norm.split(None, 1)[0].strip('"').upper()
        if first in ("CHECK", "EXCLUDE"):
            continue
        if first in ("PRIMARY", "UNIQUE", "FOREIGN"):
            parse_table_constraint(item_norm, table, model, source)
            continue

        # обычная колонка (или CREATE ... (LIKE ...) — отфильтруется выше)
        c = parse_column(item_norm, source)
        if c is None:
            continue
        if first.upper() == "LIKE":
            warn(f"LIKE-определение пропущено: {item_norm[:60]!r} ({source})")
            continue
        saved = table.add_column(c["name"], c["type"], pk=c["pk"], uk=c["uk"],
                                 fk=c["fk"], not_null=c["not_null"])
        if c["ref"]:
            model.fks.append({"child": table.name, "child_col": saved["name"],
                              "parent": entity_name(c["ref"][0]),
                              "parent_col": c["ref"][1] or saved["name"]})


def parse_table_constraint(item: str, table: Table, model: Model, source: str) -> None:
    up = item.upper()
    if up.startswith("PRIMARY"):
        m = re.search(r"PRIMARY\s+KEY\s*\(([^)]*)\)", item, re.IGNORECASE)
        if m:
            for col in m.group(1).split(","):
                c = table.col(col.strip().strip('"'))
                if c:
                    c["pk"] = True
        return
    if up.startswith("UNIQUE"):
        m = re.search(r"UNIQUE\s*(?:NULLS\s+NOT\s+DISTINCT\s*)?\(([^)]*)\)",
                      item, re.IGNORECASE)
        if m:
            for col in m.group(1).split(","):
                c = table.col(col.strip().strip('"'))
                if c:
                    c["uk"] = True
        return
    if up.startswith("FOREIGN"):
        m = re.search(r"FOREIGN\s+KEY\s*\(([^)]*)\)", item, re.IGNORECASE)
        if not m:
            warn(f"FOREIGN KEY не распознан: {item[:70]!r} ({source})")
            return
        cols = [c.strip().strip('"') for c in m.group(1).split(",")]
        add_foreign_key(cols, item, table, model, source)
        return
    # CHECK — просто пропускаем


def add_foreign_key(cols: list[str], ref_txt: str, table: Table,
                    model: Model, source: str) -> None:
    """FK из табличного ограничения/ALTER.

    Одиночный FK — одна связь с подписью колонки; составной (несколько
    колонок в одном FOREIGN KEY) — одна связь с подписью
    «кол1 + кол2 (составной FK)», Mermaid составные связи не выражает.
    """
    m = re.search(
        r"REFERENCES\s+(?:ONLY\s+)?(?:\"?[\w$]+\"?\.)?\"?([\w$]+)\"?"
        r"(?:\s*\(([^)]*)\))?",
        ref_txt, re.IGNORECASE)
    if not m:
        warn(f"REFERENCES не распознан в FK: {ref_txt[:70]!r} ({source})")
        return
    parent = entity_name(m.group(1))
    ref_cols = ([c.strip().strip('"') for c in m.group(2).split(",")]
                if m.group(2) else [])
    if len(cols) == 1:
        model.fks.append({"child": table.name, "child_col": cols[0],
                          "parent": parent,
                          "parent_col": ref_cols[0] if ref_cols else cols[0]})
    else:
        pairs = [(col, ref_cols[i] if i < len(ref_cols) else col)
                 for i, col in enumerate(cols)]
        model.fks.append({
            "child": table.name, "parent": parent, "composite": pairs,
            "child_col": " + ".join(cols),
            "parent_col": " + ".join(p for _, p in pairs),
        })
    for col in cols:
        c = table.col(col)
        if c:
            c["fk"] = True


def parse_alter_table(stmt: str, model: Model, source: str) -> None:
    m = re.match(r"ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?"
                 r"(\"?[\w$]+\"?(?:\.[\w$\".]+)?)\s+(.*)$",
                 stmt, re.IGNORECASE | re.DOTALL)
    if not m:
        warn(f"ALTER TABLE не распознан: {stmt[:60]!r} ({source})")
        return
    table = model.table(entity_name(m.group(1)))  # стаб, если CREATE не в файлах
    rest = m.group(2)

    # ADD CONSTRAINT ... FOREIGN KEY / PRIMARY KEY / UNIQUE
    m2 = re.search(
        r"ADD\s+(?:CONSTRAINT\s+\"?([\w$]+)\"?\s+)?"
        r"(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE)\s*\(([^)]*)\)"
        r"(\s+REFERENCES.*)?",
        rest, re.IGNORECASE | re.DOTALL)
    if m2:
        kind = m2.group(2).upper().replace(" ", " ")
        cols = [c.strip().strip('"') for c in m2.group(3).split(",")]
        if kind == "PRIMARY KEY":
            for col in cols:
                c = table.col(col)
                if c:
                    c["pk"] = True
            return
        if kind == "UNIQUE":
            for col in cols:
                c = table.col(col)
                if c:
                    c["uk"] = True
            return
        ref_txt = m2.group(4) or ""
        add_foreign_key(cols, ref_txt, table, model, source)
        return

    # ADD [COLUMN] [IF NOT EXISTS] имя тип ...
    m3 = re.search(r"ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(.+)$",
                   rest, re.IGNORECASE | re.DOTALL)
    if m3:
        c = parse_column(re.sub(r"\s+", " ", m3.group(1)).strip(), source)
        if c:
            saved = table.add_column(c["name"], c["type"], pk=c["pk"], uk=c["uk"],
                                     fk=c["fk"], not_null=c["not_null"])
            if c["ref"]:
                model.fks.append({"child": table.name, "child_col": saved["name"],
                                  "parent": entity_name(c["ref"][0]),
                                  "parent_col": c["ref"][1] or saved["name"]})
        return
    warn(f"ALTER TABLE без ADD CONSTRAINT/COLUMN пропущен: {stmt[:60]!r} ({source})")


def warn(msg: str) -> None:
    print(f"ПРЕДУПРЕЖДЕНИЕ: {msg}", file=sys.stderr)


# --------------------------------------------------------------------- вывод

def render(model: Model) -> str:
    lines = ["erDiagram"]
    for name in model.order:
        t = model.tables[name]
        if not t.columns:
            warn(f"у сущности {name} нет колонок (стаб по FK без CREATE TABLE)")
        lines.append(f"    {name} {{")
        for c in t.columns:
            keys = ""
            if c["pk"]:
                keys += " PK"
            if c["fk"]:
                keys += " FK"
            elif c["uk"]:
                keys += " UK"
            lines.append(f"        {c['type']} {c['name']}{keys}")
        lines.append("    }")
    if model.fks:
        lines.append("")
    for fk in model.fks:
        # один-ко-многим: у родителя ||, у ребёнка o{ (FK обычно nullable/множественный)
        if fk.get("composite"):
            label = " + ".join(c for c, _ in fk["composite"]) + " (составной FK)"
            lines.append(f'    {fk["parent"]} ||--o{{ {fk["child"]} : "{label}"')
        else:
            lines.append(f'    {fk["parent"]} ||--o{{ {fk["child"]} : {fk["child_col"]}')
    return "\n".join(lines) + "\n"


# ---------------------------------------------------------------------- main

def parse_sql_text(sql: str, model: Model, source: str) -> None:
    clean = strip_comments(sql)
    for stmt in split_statements(clean):
        head = re.sub(r"\s+", " ", stmt[:80]).upper()
        if re.match(r"CREATE\s+(?:UNLOGGED\s+)?TABLE\b", stmt, re.IGNORECASE):
            parse_create_table(stmt, model, source)
        elif head.startswith("ALTER TABLE"):
            parse_alter_table(stmt, model, source)
        # прочее (CREATE INDEX, INSERT, ...) — игнорируем молча


def main() -> int:
    # stderr на Windows-консоли может быть в cp1251/cp866 — принудительно utf-8
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    ap = argparse.ArgumentParser(
        description="Генерация Mermaid erDiagram из SQL-миграций (stdlib-only).")
    ap.add_argument("files", nargs="*", help="SQL-файлы миграций (по порядку)")
    ap.add_argument("--dir", help="каталог миграций: берёт *.sql отсортированно")
    ap.add_argument("-o", "--out", help="файл вывода (default: stdout)")
    args = ap.parse_args()

    paths: list[str] = list(args.files)
    if args.dir:
        paths.extend(sorted(glob.glob(os.path.join(args.dir, "*.sql"))))
    if not paths:
        ap.error("укажите SQL-файлы или --dir")

    model = Model()
    for path in paths:
        try:
            with open(path, encoding="utf-8") as f:
                parse_sql_text(f.read(), model, os.path.basename(path))
        except OSError as e:
            warn(f"файл не прочитан: {path}: {e}")

    out = render(model)
    if args.out:
        os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
        with open(args.out, "w", encoding="utf-8", newline="\n") as f:
            f.write(out)
        print(f"OK: {args.out} ({len(model.order)} сущностей, "
              f"{len(model.fks)} связей)", file=sys.stderr)
    else:
        sys.stdout.write(out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
