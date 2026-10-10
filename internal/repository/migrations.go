package repository

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/pressly/goose/v3"
)

// //go:embed — директива компилятора: при сборке вшить все .sql из указанной
// папки в бинарник. migrationsFS становится виртуальной ФС (fs.FS),
// из которой goose будет читать миграции уже без диска.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate приводит схему базы к актуальному состоянию: накатывает все
// ещё не применённые миграции по порядку. Идемпотентна — безопасно
// вызывать при каждом старте сервиса.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Goose requires database/sql; the adapter uses the existing pgx pool.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	// Указываем goose диалект: генерируемые им запросы и их обработка
	// зависят от СУБД.
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("установка диалекта goose: %w", err)
	}

	// Переводим goose на встроенную ФС вместо реального диска.
	// Путь "migrations" — виртуальный корень внутри migrationsFS
	// (в эту ФС вшита только папка migrations, так что путь относительный).
	goose.SetBaseFS(migrationsFS)

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("применение миграций: %w", err)
	}

	return nil
}
