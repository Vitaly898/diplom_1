package accrual

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestClientGetOrder(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		body       string
		retryAfter string
		status     string
		amount     *model.Money
		wantErr    bool
		notFound   bool
		rateLimit  bool
	}{
		{name: "registered", code: 200, body: `{"order":"123","status":"REGISTERED"}`, status: "PROCESSING"},
		{name: "processing", code: 200, body: `{"order":"123","status":"PROCESSING"}`, status: "PROCESSING"},
		{name: "invalid", code: 200, body: `{"order":"123","status":"INVALID"}`, status: "INVALID"},
		{name: "processed", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":10.25}`, status: "PROCESSED", amount: moneyPointer(1025)},
		{name: "zero", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":0}`, status: "PROCESSED", amount: moneyPointer(0)},
		{name: "no accrual", code: 200, body: `{"order":"123","status":"PROCESSED"}`, status: "PROCESSED"},
		{name: "not registered", code: 204, wantErr: true, notFound: true},
		{name: "rate limit", code: 429, retryAfter: "2", wantErr: true, rateLimit: true},
		{name: "server error", code: 500, wantErr: true},
		{name: "unexpected status", code: 404, wantErr: true},
		{name: "malformed json", code: 200, body: `{`, wantErr: true},
		{name: "wrong order", code: 200, body: `{"order":"456","status":"PROCESSED"}`, wantErr: true},
		{name: "unknown status", code: 200, body: `{"order":"123","status":"UNKNOWN"}`, wantErr: true},
		{name: "negative accrual", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":-1}`, wantErr: true},
		{name: "too precise", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":0.001}`, wantErr: true},
		{name: "quoted accrual", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":"10.25"}`, wantErr: true},
		{name: "outside database range", code: 200, body: `{"order":"123","status":"PROCESSED","accrual":10000000000}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("http://accrual.test/")
			if err != nil {
				t.Fatal(err)
			}
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.String() != "http://accrual.test/api/orders/123" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
				return &http.Response{
					StatusCode: tt.code,
					Header:     http.Header{"Retry-After": []string{tt.retryAfter}},
					Body:       io.NopCloser(strings.NewReader(tt.body)),
				}, nil
			})
			result, err := client.GetOrder(context.Background(), "123")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.notFound && !errors.Is(err, ErrNotRegistered) {
				t.Fatalf("expected ErrNotRegistered, got %v", err)
			}
			if tt.rateLimit {
				var rateLimit *RateLimitError
				if !errors.As(err, &rateLimit) || rateLimit.RetryAfter != 2*time.Second {
					t.Fatalf("unexpected rate limit error: %v", err)
				}
			}
			if !tt.wantErr {
				if result.Status != tt.status || (result.Accrual == nil) != (tt.amount == nil) {
					t.Fatalf("unexpected result: %+v", result)
				}
				if tt.amount != nil && *result.Accrual != *tt.amount {
					t.Fatalf("amount=%d, want %d", *result.Accrual, *tt.amount)
				}
			}
		})
	}
}

func moneyPointer(amount model.Money) *model.Money {
	return &amount
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		input string
		want  time.Duration
	}{
		{"15", 15 * time.Second},
		{"0", 0},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-time.Second).Format(http.TimeFormat), 0},
		{"", time.Minute},
		{"bad", time.Minute},
		{"-1", time.Minute},
		{"9223372036854775807", time.Minute},
	} {
		if got := parseRetryAfter(tt.input, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q)=%s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestClientCancellation(t *testing.T) {
	client, err := NewClient("http://accrual.test")
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetOrder(ctx, "123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
