// Package migrations — только встраивание SQL-файлов миграций в бинарник
// notifier (go:embed не умеет подниматься выше каталога пакета). Сами файлы
// не редактировать: схему БД меняют только миграции Ильи (issue #31, этап 2).
package migrations

import "embed"

//go:embed *.sql
var SQL embed.FS

// FS возвращает встраиваемую файловую систему с SQL-миграциями.
func FS() embed.FS { return SQL }
