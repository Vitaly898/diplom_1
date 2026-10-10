// Package repository provides PostgreSQL persistence.
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a pool and checks connectivity before returning it.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("создание пула соединений: %w", err)
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("подключение к базе данных: %w", err)
	}
	return db, nil
}
