package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/repository"
	"github.com/Vitaly898/diplom_1/internal/service"
)

type balanceRepositoryStub struct {
	balance model.Balance
	history []model.Withdrawal
	err     error
	calls   int
	userID  int64
	order   string
	sum     model.Money
}

func (s *balanceRepositoryStub) GetBalance(context.Context, int64) (model.Balance, error) {
	return s.balance, s.err
}

func (s *balanceRepositoryStub) GetWithdrawalsByUser(context.Context, int64) ([]model.Withdrawal, error) {
	return s.history, s.err
}

func (s *balanceRepositoryStub) Withdraw(_ context.Context, userID int64, order string, sum model.Money) error {
	s.calls++
	s.userID, s.order, s.sum = userID, order, sum
	return s.err
}

func balanceTestRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), userIDKey, int64(42)))
}

func TestWithdraw(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		err    error
		status int
		calls  int
	}{
		{name: "success", body: `{"order":"2377225624","sum":10.25}`, status: 200, calls: 1},
		{name: "insufficient funds", body: `{"order":"2377225624","sum":10.25}`, err: fmt.Errorf("wrapped: %w", repository.ErrInsufficientFunds), status: 402, calls: 1},
		{name: "database error", body: `{"order":"2377225624","sum":10.25}`, err: errors.New("database unavailable"), status: 500, calls: 1},
		{name: "invalid order", body: `{"order":"123","sum":10.25}`, status: 422},
		{name: "zero", body: `{"order":"2377225624","sum":0}`, status: 400},
		{name: "negative", body: `{"order":"2377225624","sum":-1}`, status: 400},
		{name: "missing sum", body: `{"order":"2377225624"}`, status: 400},
		{name: "null sum", body: `{"order":"2377225624","sum":null}`, status: 400},
		{name: "quoted sum", body: `{"order":"2377225624","sum":"10.25"}`, status: 400},
		{name: "too precise", body: `{"order":"2377225624","sum":0.001}`, status: 400},
		{name: "invalid json", body: `{`, status: 400},
		{name: "extra json", body: `{"order":"2377225624","sum":10.25} {}`, status: 400},
		{name: "trailing garbage", body: `{"order":"2377225624","sum":10.25} x`, status: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &balanceRepositoryStub{err: tt.err}
			h := newBalanceHandler(service.NewBalanceService(repo))
			w := httptest.NewRecorder()
			h.withdraw(w, balanceTestRequest(http.MethodPost, "/api/user/balance/withdraw", tt.body))
			if w.Code != tt.status || repo.calls != tt.calls {
				t.Fatalf("status=%d, calls=%d; want status=%d, calls=%d", w.Code, repo.calls, tt.status, tt.calls)
			}
			if repo.calls > 0 && (repo.userID != 42 || repo.order != "2377225624" || repo.sum != 1025) {
				t.Fatalf("unexpected repository arguments: user=%d order=%q sum=%d", repo.userID, repo.order, repo.sum)
			}
		})
	}
}

func TestBalanceResponse(t *testing.T) {
	for _, balance := range []model.Balance{{}, {Current: 7025, Withdrawn: 3000}} {
		repo := &balanceRepositoryStub{balance: balance}
		h := newBalanceHandler(service.NewBalanceService(repo))
		w := httptest.NewRecorder()
		h.balance(w, balanceTestRequest(http.MethodGet, "/api/user/balance", ""))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected response: %d %v", w.Code, w.Header())
		}
		var response map[string]any
		decoder := json.NewDecoder(w.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response["current"] != json.Number(balance.Current.String()) || response["withdrawn"] != json.Number(balance.Withdrawn.String()) {
			t.Fatalf("unexpected JSON amounts: %v", response)
		}
	}
}

func TestWithdrawalsResponse(t *testing.T) {
	repo := &balanceRepositoryStub{}
	h := newBalanceHandler(service.NewBalanceService(repo))
	w := httptest.NewRecorder()
	h.withdrawals(w, balanceTestRequest(http.MethodGet, "/api/user/withdrawals", ""))
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("unexpected empty history response: %d %s", w.Code, w.Body.String())
	}
	repo.history = []model.Withdrawal{{OrderNumber: "2377225624", Sum: 1025, ProcessedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}}
	w = httptest.NewRecorder()
	h.withdrawals(w, balanceTestRequest(http.MethodGet, "/api/user/withdrawals", ""))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected history response: %d %v", w.Code, w.Header())
	}
	want := `[{"order":"2377225624","sum":10.25,"processed_at":"2026-10-03T12:00:00Z"}]`
	if strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("unexpected history: %s", w.Body.String())
	}
}

func TestBalanceRoutesRequireAuth(t *testing.T) {
	router := NewRouter(nil, nil, service.NewBalanceService(&balanceRepositoryStub{}), auth.NewTokenService("test-secret"))
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/user/balance"},
		{http.MethodPost, "/api/user/balance/withdraw"},
		{http.MethodGet, "/api/user/withdrawals"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Errorf("%s %s: status=%d, want 401", route.method, route.path, w.Code)
		}
	}
}
