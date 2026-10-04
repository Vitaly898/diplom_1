package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Vitaly898/diplom_1/internal/model"
)

var ErrInsufficientFunds = errors.New("недостаточно баллов")

type BalanceRepository struct {
	db *sql.DB
}

func NewBalanceRepository(db *sql.DB) *BalanceRepository {
	return &BalanceRepository{db: db}
}

func (r *BalanceRepository) GetBalance(ctx context.Context, userID int64) (model.Balance, error) {
	var currentText, withdrawnText string

	err := r.db.QueryRowContext(ctx, `
		WITH totals AS (
			SELECT
				(
					SELECT COALESCE(SUM(accrual), 0)
					FROM orders
					WHERE user_id = $1 AND status = 'PROCESSED'
				) AS earned,
				(
					SELECT COALESCE(SUM(sum), 0)
					FROM withdrawals
					WHERE user_id = $1
				) AS withdrawn
		)
		SELECT (earned - withdrawn)::text, withdrawn::text
		FROM totals
	`, userID).Scan(&currentText, &withdrawnText)

	if err != nil {
		return model.Balance{}, fmt.Errorf("ошибка получения баланса: %w", err)
	}

	current, err := model.ParseMoney(currentText)
	if err != nil {
		return model.Balance{}, fmt.Errorf("ошибка получения текущего баланса: %w", err)
	}
	withdrawn, err := model.ParseMoney(withdrawnText)
	if err != nil {
		return model.Balance{}, fmt.Errorf("ошибка получения суммы списаний: %w", err)
	}

	return model.Balance{
		Current:   current,
		Withdrawn: withdrawn,
	}, nil

}

func (r *BalanceRepository) GetWithdrawalsByUser(ctx context.Context, userID int64) ([]model.Withdrawal, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, order_number, sum::text,processed_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY processed_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка списаний: %w", err)
	}
	defer rows.Close()

	var withdrawals []model.Withdrawal

	for rows.Next() {
		var withdrawal model.Withdrawal
		var sumText string

		err := rows.Scan(
			&withdrawal.ID,
			&withdrawal.UserID,
			&withdrawal.OrderNumber,
			&sumText,
			&withdrawal.ProcessedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("ошибка чтения списания: %w", err)
		}
		withdrawal.Sum, err = model.ParseMoney(sumText)
		if err != nil {
			return nil, fmt.Errorf("чтение суммы списания: %w", err)
		}

		withdrawals = append(withdrawals, withdrawal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход списка списаний: %w", err)
	}

	return withdrawals, nil

}

func (r *BalanceRepository) Withdraw(ctx context.Context, userID int64, orderNumber string, sum model.Money) error {
	if sum <= 0 {
		return fmt.Errorf("сумма списания должна быть положительной")
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("начало транзакции списания: %w", err)
	}

	defer tx.Rollback()

	var lockedUserID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&lockedUserID)
	if err != nil {
		return fmt.Errorf("блокировка пользователя: %w", err)
	}

	var currentText string
	err = tx.QueryRowContext(ctx, `
		SELECT (
			(
				SELECT COALESCE(SUM(accrual), 0)
				FROM orders
				WHERE user_id = $1 AND status = 'PROCESSED'
			)
			-
			(
				SELECT COALESCE(SUM(sum), 0)
				FROM withdrawals
				WHERE user_id = $1
			)
		)::text
	`, userID).Scan(&currentText)
	if err != nil {
		return fmt.Errorf("проверка баланса перед списанием: %w", err)
	}

	current, err := model.ParseMoney(currentText)
	if err != nil {
		return fmt.Errorf("чтение баланса перед списанием: %w", err)
	}
	if current < sum {
		return ErrInsufficientFunds
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO withdrawals (user_id, order_number, sum)
		VALUES ($1, $2, $3::numeric)
	`, userID, orderNumber, sum.String())
	if err != nil {
		return fmt.Errorf("сохранение списания: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("подтверждение списания: %w", err)
	}
	return nil
}
