package repository

import (
	"context"
	"fmt"

	"github.com/Vitaly898/diplom_1/internal/model"
)

func (r *OrderRepository) GetPendingOrderNumbers(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT number
		FROM orders
		WHERE status IN ('NEW', 'PROCESSING')
		ORDER BY uploaded_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("получение необработанных заказов: %w", err)
	}
	defer rows.Close()

	var numbers []string
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, fmt.Errorf("чтение номера необработанного заказа: %w", err)
		}
		numbers = append(numbers, number)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход необработанных заказов: %w", err)
	}
	return numbers, nil
}

func (r *OrderRepository) UpdateOrderAccrual(ctx context.Context, number, status string, amount *model.Money) error {
	if status != "PROCESSING" && status != "PROCESSED" && status != "INVALID" {
		return fmt.Errorf("недопустимый статус заказа: %q", status)
	}
	var value any
	if status == "PROCESSED" && amount != nil {
		if *amount < 0 || *amount > 999999999999 {
			return fmt.Errorf("сумма начисления вне диапазона NUMERIC(12,2)")
		}
		value = amount.String()
	}
	_, err := r.db.Exec(ctx, `
		UPDATE orders
		SET status = $2, accrual = $3::numeric
		WHERE number = $1 AND status IN ('NEW', 'PROCESSING')
	`, number, status, value)
	if err != nil {
		return fmt.Errorf("обновление начисления заказа: %w", err)
	}
	return nil
}
