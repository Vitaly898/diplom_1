package accrual

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
)

type workerRepositoryStub struct {
	numbers []string
	updated map[string]Result
}

func (r *workerRepositoryStub) GetPendingOrderNumbers(context.Context) ([]string, error) {
	return r.numbers, nil
}

func (r *workerRepositoryStub) UpdateOrderAccrual(_ context.Context, number, status string, amount *model.Money) error {
	r.updated[number] = Result{Status: status, Accrual: amount}
	return nil
}

type orderClientFunc func(context.Context, string) (Result, error)

func (f orderClientFunc) GetOrder(ctx context.Context, number string) (Result, error) {
	return f(ctx, number)
}

func TestWorkerPollContinuesAfterFailure(t *testing.T) {
	repo := &workerRepositoryStub{
		numbers: []string{"not-found", "failed", "processing", "processed", "invalid"},
		updated: make(map[string]Result),
	}
	remoteErr := errors.New("HTTP 500")
	client := orderClientFunc(func(_ context.Context, number string) (Result, error) {
		switch number {
		case "not-found":
			return Result{}, ErrNotRegistered
		case "failed":
			return Result{}, remoteErr
		case "processing":
			return Result{Status: "PROCESSING"}, nil
		case "processed":
			return Result{Status: "PROCESSED", Accrual: moneyPointer(1025)}, nil
		default:
			return Result{Status: "INVALID"}, nil
		}
	})
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.poll(context.Background()); !errors.Is(err, remoteErr) {
		t.Fatalf("expected remote error, got %v", err)
	}
	if len(repo.updated) != 3 || repo.updated["processing"].Status != "PROCESSING" || repo.updated["invalid"].Status != "INVALID" {
		t.Fatalf("unexpected updates: %v", repo.updated)
	}
	processed := repo.updated["processed"]
	if processed.Status != "PROCESSED" || processed.Accrual == nil || *processed.Accrual != 1025 {
		t.Fatalf("unexpected accrual: %+v", processed)
	}
}

func TestWorkerRateLimitStopsEntirePoll(t *testing.T) {
	repo := &workerRepositoryStub{numbers: []string{"first", "second"}, updated: make(map[string]Result)}
	calls := 0
	client := orderClientFunc(func(context.Context, string) (Result, error) {
		calls++
		return Result{}, &RateLimitError{RetryAfter: time.Hour}
	})
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var rateLimit *RateLimitError
	if err := worker.poll(context.Background()); !errors.As(err, &rateLimit) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
	if calls != 1 || len(repo.updated) != 0 {
		t.Fatalf("calls=%d, updates=%v", calls, repo.updated)
	}
}

func TestWorkerShutdownInterruptsRetryAfter(t *testing.T) {
	repo := &workerRepositoryStub{numbers: []string{"first"}, updated: make(map[string]Result)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{})
	client := orderClientFunc(func(context.Context, string) (Result, error) {
		close(called)
		return Result{}, &RateLimitError{RetryAfter: time.Hour}
	})
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("worker did not poll")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestWorkerRetriesAfterPause(t *testing.T) {
	for _, tt := range []struct {
		name         string
		err          error
		minimumDelay time.Duration
	}{
		{name: "rate limit", err: &RateLimitError{RetryAfter: 200 * time.Millisecond}, minimumDelay: 200 * time.Millisecond},
		{name: "server error", err: errors.New("HTTP 500"), minimumDelay: time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &workerRepositoryStub{numbers: []string{"first"}, updated: make(map[string]Result)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := make(chan time.Time, 10)
			client := orderClientFunc(func(context.Context, string) (Result, error) {
				requests <- time.Now()
				return Result{}, tt.err
			})
			worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
			done := make(chan struct{})
			go func() { defer close(done); worker.Run(ctx) }()
			defer func() { cancel(); <-done }()
			var first, second time.Time
			select {
			case first = <-requests:
			case <-time.After(3 * time.Second):
				t.Fatal("initial request missing")
			}
			select {
			case second = <-requests:
			case <-time.After(3 * time.Second):
				t.Fatal("retry missing")
			}
			if delay := second.Sub(first); delay < tt.minimumDelay {
				t.Fatalf("retried too early: %s, want at least %s", delay, tt.minimumDelay)
			}
		})
	}
}
