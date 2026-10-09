package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Vitaly898/diplom_1/internal/model"
)

var (
	ErrOrderExists   = errors.New("заказ с таким номером уже существует")
	ErrOrderNotFound = errors.New("заказ не найден")
)

type OrderRepository struct {
	db *pgxpool.Pool
}

func NewOrderRepository(db *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{db: db}
}

// CreateOrder сохраняет новый заказ. Статус NEW и uploaded_at
// проставляет DEFAULT из схемы БД. UNIQUE-конфликт номера ловим
// по коду 23505: защита от дубликатов атомарна, на стороне БД.
func (r *OrderRepository) CreateOrder(ctx context.Context, userID int64, number string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO orders (user_id, number) VALUES ($1, $2)`, userID, number)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrOrderExists
		}
		return fmt.Errorf("создание заказа: %w", err)
	}
	return nil
}

func (r *OrderRepository) GetOrderByNumber(ctx context.Context, number string) (*model.Order, error) {
	var o model.Order
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, number, status, accrual, uploaded_at
		 FROM orders WHERE number = $1`, number,
	).Scan(&o.ID, &o.UserID, &o.Number, &o.Status, &o.Accrual, &o.UploadedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("поиск заказа: %w", err)
	}
	return &o, nil
}

// Исправлено имя: GetOrdersByUser (множественное число — возвращаем
// список), чтобы совпадать с интерфейсом в service.
func (r *OrderRepository) GetOrdersByUser(ctx context.Context, userID int64) ([]model.Order, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, number, status, accrual, uploaded_at
		 FROM orders WHERE user_id = $1
		 ORDER BY uploaded_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("список заказов: %w", err)
	}
	defer rows.Close()

	var orders []model.Order
	for rows.Next() {
		var o model.Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Number, &o.Status, &o.Accrual, &o.UploadedAt); err != nil {
			return nil, fmt.Errorf("чтение заказа: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход результата: %w", err)
	}
	return orders, nil
}
