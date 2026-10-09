package accrual

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
)

type workerRepositoryStub struct {
	mu      sync.Mutex
	numbers []string
	updated map[string]Result
}

func (r *workerRepositoryStub) GetPendingOrderNumbers(context.Context) ([]string, error) {
	return r.numbers, nil
}

func (r *workerRepositoryStub) UpdateOrderAccrual(_ context.Context, number, status string, amount *model.Money) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	var calls atomic.Int32
	client := orderClientFunc(func(context.Context, string) (Result, error) {
		calls.Add(1)
		return Result{}, &RateLimitError{RetryAfter: time.Hour}
	})
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var rateLimit *RateLimitError
	if err := worker.poll(context.Background()); !errors.As(err, &rateLimit) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
	if calls.Load() < 1 || calls.Load() > 2 || len(repo.updated) != 0 {
		t.Fatalf("calls=%d, updates=%v", calls.Load(), repo.updated)
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

func TestWorkerPoolBoundedConcurrency(t *testing.T) {
	repo := &workerRepositoryStub{numbers: []string{"1", "2", "3", "4", "5", "6", "7", "8"}, updated: make(map[string]Result)}
	entered := make(chan struct{}, len(repo.numbers))
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	client := orderClientFunc(func(ctx context.Context, number string) (Result, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
			return Result{Status: "PROCESSING"}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- worker.poll(ctx) }()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("pool did not issue four concurrent requests")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 4 || len(repo.updated) != len(repo.numbers) {
		t.Fatalf("peak=%d updates=%d", peak.Load(), len(repo.updated))
	}
}

func TestWorkerRateLimitCancelsInflightRequests(t *testing.T) {
	repo := &workerRepositoryStub{numbers: []string{"1", "2", "3", "4", "5", "6"}, updated: make(map[string]Result)}
	var calls atomic.Int32
	var cancelled atomic.Int32
	ready := make(chan struct{})
	client := orderClientFunc(func(ctx context.Context, number string) (Result, error) {
		n := calls.Add(1)
		if n == 4 {
			close(ready)
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
		if n == 1 {
			return Result{}, &RateLimitError{RetryAfter: time.Hour}
		}
		<-ctx.Done()
		cancelled.Add(1)
		return Result{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	worker := NewWorker(repo, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := worker.poll(ctx)
	var limit *RateLimitError
	if !errors.As(err, &limit) || limit.RetryAfter < 59*time.Minute || calls.Load() != 4 || cancelled.Load() != 3 {
		t.Fatalf("err=%v calls=%d cancelled=%d", err, calls.Load(), cancelled.Load())
	}
}
