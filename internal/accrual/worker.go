package accrual

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
)

type OrderRepository interface {
	GetPendingOrderNumbers(ctx context.Context) ([]string, error)
	UpdateOrderAccrual(ctx context.Context, number, status string, amount *model.Money) error
}

type OrderClient interface {
	GetOrder(ctx context.Context, number string) (Result, error)
}

type Worker struct {
	repo   OrderRepository
	client OrderClient
	logger *slog.Logger
}

func NewWorker(repo OrderRepository, client OrderClient, logger *slog.Logger) *Worker {
	return &Worker{repo: repo, client: client, logger: logger}
}

func (w *Worker) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := w.poll(ctx)
		if ctx.Err() != nil {
			return
		}
		delay := time.Second
		if err != nil {
			var rateLimit *RateLimitError
			if errors.As(err, &rateLimit) {
				delay = rateLimit.RetryAfter
				w.logger.Warn("опрос начислений приостановлен", "retry_after", delay)
			} else {
				delay = backoff
				backoff = min(backoff*2, 30*time.Second)
				w.logger.Warn("ошибка опроса начислений", "error", err, "retry_after", delay)
			}
		} else {
			backoff = time.Second
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) poll(ctx context.Context) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	numbers, err := w.repo.GetPendingOrderNumbers(queryCtx)
	cancel()
	if err != nil {
		return err
	}

	var firstErr error
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := w.client.GetOrder(ctx, number)
		if errors.Is(err, ErrNotRegistered) {
			continue
		}
		if err != nil {
			var rateLimit *RateLimitError
			if errors.As(err, &rateLimit) {
				return err
			}
			w.logger.Warn("не удалось получить начисление", "order", number, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = w.repo.UpdateOrderAccrual(updateCtx, number, result.Status, result.Accrual)
		cancel()
		if err != nil {
			w.logger.Warn("не удалось сохранить начисление", "order", number, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
