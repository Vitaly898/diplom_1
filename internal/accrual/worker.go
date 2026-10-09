package accrual

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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

const workerPoolSize = 4

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

	// Cancelling the shared context interrupts requests and client retry timers.
	pollCtx, stop := context.WithCancel(ctx)
	defer stop()
	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var pauseUntil time.Time
	recordError := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		var rateLimit *RateLimitError
		if errors.As(err, &rateLimit) {
			deadline := time.Now().Add(rateLimit.RetryAfter)
			if deadline.After(pauseUntil) {
				pauseUntil = deadline
			}
			stop()
		} else if firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < min(workerPoolSize, len(numbers)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for number := range jobs {
				if pollCtx.Err() != nil {
					return
				}
				result, err := w.client.GetOrder(pollCtx, number)
				if errors.Is(err, ErrNotRegistered) {
					continue
				}
				if err != nil {
					recordError(err)
					if pollCtx.Err() != nil {
						return
					}
					w.logger.Warn("не удалось получить начисление", "order", number, "error", err)
					continue
				}
				updateCtx, cancel := context.WithTimeout(pollCtx, 5*time.Second)
				err = w.repo.UpdateOrderAccrual(updateCtx, number, result.Status, result.Accrual)
				cancel()
				if err != nil {
					recordError(err)
					w.logger.Warn("не удалось сохранить начисление", "order", number, "error", err)
				}
			}
		}()
	}
enqueue:
	for _, number := range numbers {
		select {
		case <-pollCtx.Done():
			break enqueue
		case jobs <- number:
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !pauseUntil.IsZero() {
		return &RateLimitError{RetryAfter: max(0, time.Until(pauseUntil))}
	}
	return firstErr
}
