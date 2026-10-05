package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Vitaly898/diplom_1/internal/model"
)

// Sentinel-ошибки слоя: сервис отличает их через errors.Is.
var (
	ErrLoginExists  = errors.New("логин уже занят")
	ErrUserNotFound = errors.New("пользователь не найден")
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// CreateUser создаёт пользователя, возвращает его ID.
// UNIQUE-конфликт логина ловим по коду ошибки Postgres 23505 —
// защита от дубликатов атомарна даже при одновременных вставках.
func (r *UserRepository) CreateUser(ctx context.Context, login, passwordHash string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (login, password_hash) VALUES ($1, $2) RETURNING id`,
		login, passwordHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrLoginExists
		}
		return 0, fmt.Errorf("создание пользователя: %w", err)
	}
	return id, nil
}

func (r *UserRepository) GetUserByLogin(ctx context.Context, login string) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, login, password_hash, created_at FROM users WHERE login = $1`,
		login,
	).Scan(&u.ID, &u.Login, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		// sql.ErrNoRows — "не нашли строку", это НЕ сбой БД.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("поиск пользователя: %w", err)
	}
	return &u, nil
}
