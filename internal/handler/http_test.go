package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/service"
)

type userServiceStub struct {
	err   error
	calls int
}

func (s *userServiceStub) Register(_ context.Context, login, password string) (string, error) {
	s.calls++
	return "test-token", s.err
}
func (s *userServiceStub) Login(_ context.Context, login, password string) (string, error) {
	s.calls++
	return "test-token", s.err
}

func TestAuthHandlers(t *testing.T) {
	for _, method := range []string{"register", "login"} {
		for _, tt := range []struct {
			name, body    string
			err           error
			status, calls int
		}{
			{name: "success", body: `{"login":"alice","password":"secret"}`, status: 200, calls: 1},
			{name: "invalid json", body: `{`, status: 400},
			{name: "missing login", body: `{"password":"secret"}`, status: 400},
			{name: "missing password", body: `{"login":"alice"}`, status: 400},
			{name: "duplicate", body: `{"login":"alice","password":"secret"}`, err: service.ErrUserExists, status: 409, calls: 1},
			{name: "credentials", body: `{"login":"alice","password":"secret"}`, err: service.ErrInvalidCredentials, status: 401, calls: 1},
			{name: "failure", body: `{"login":"alice","password":"secret"}`, err: errors.New("database unavailable"), status: 500, calls: 1},
		} {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				svc := &userServiceStub{err: tt.err}
				h := newAuthHandler(svc)
				r := httptest.NewRequest(http.MethodPost, "/api/user/"+method, strings.NewReader(tt.body))
				w := httptest.NewRecorder()
				if method == "register" {
					h.register(w, r)
				} else {
					h.login(w, r)
				}
				if w.Code != tt.status || svc.calls != tt.calls {
					t.Fatalf("status=%d calls=%d", w.Code, svc.calls)
				}
				if tt.status == 200 {
					var body authResponse
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Token != "test-token" {
						t.Fatalf("invalid response: %s", w.Body.String())
					}
					cookies := w.Result().Cookies()
					if len(cookies) != 1 || cookies[0].Name != "token" || cookies[0].Value != body.Token || !cookies[0].HttpOnly || cookies[0].Path != "/" {
						t.Fatalf("invalid cookies: %v", cookies)
					}
				}
			})
		}
	}
}

type orderServiceStub struct {
	already bool
	err     error
	orders  []model.Order
	userID  int64
	number  string
}

func (s *orderServiceStub) UploadOrder(_ context.Context, userID int64, number string) (bool, error) {
	s.userID, s.number = userID, number
	return s.already, s.err
}
func (s *orderServiceStub) ListOrders(context.Context, int64) ([]model.Order, error) {
	return s.orders, s.err
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestOrderUploadHandler(t *testing.T) {
	for _, tt := range []struct {
		name, body                string
		already                   bool
		err                       error
		status                    int
		unauthorized, readFailure bool
	}{
		{name: "new", body: "2377225624", status: 202},
		{name: "duplicate", body: "2377225624", already: true, status: 200},
		{name: "invalid", body: "123", err: service.ErrInvalidOrderNumber, status: 422},
		{name: "taken", body: "2377225624", err: service.ErrOrderTaken, status: 409},
		{name: "failure", body: "2377225624", err: errors.New("database unavailable"), status: 500},
		{name: "empty", status: 400},
		{name: "read failure", readFailure: true, status: 400},
		{name: "unauthorized", body: "2377225624", unauthorized: true, status: 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &orderServiceStub{already: tt.already, err: tt.err}
			r := balanceTestRequest(http.MethodPost, "/api/user/orders", tt.body)
			if tt.unauthorized {
				r = httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader(tt.body))
			}
			if tt.readFailure {
				r.Body = io.NopCloser(failingReader{})
			}
			w := httptest.NewRecorder()
			newOrderHandler(svc).upload(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d want %d", w.Code, tt.status)
			}
			if tt.status == 200 || tt.status == 202 {
				if svc.userID != 42 || svc.number != tt.body {
					t.Fatal("wrong service arguments")
				}
			}
		})
	}
}

func TestOrderListHandler(t *testing.T) {
	amount := 10.25
	date := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name         string
		orders       []model.Order
		err          error
		status       int
		unauthorized bool
	}{
		{name: "empty", status: 204},
		{name: "failure", err: errors.New("database unavailable"), status: 500},
		{name: "unauthorized", status: 401, unauthorized: true},
		{name: "list", orders: []model.Order{{Number: "2377225624", Status: "PROCESSED", Accrual: &amount, UploadedAt: date}, {Number: "12345678903", Status: "NEW", UploadedAt: date}}, status: 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := balanceTestRequest(http.MethodGet, "/api/user/orders", "")
			if tt.unauthorized {
				r = httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
			}
			w := httptest.NewRecorder()
			newOrderHandler(&orderServiceStub{orders: tt.orders, err: tt.err}).list(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d want %d", w.Code, tt.status)
			}
			if tt.status == 204 && w.Body.Len() != 0 {
				t.Fatal("204 response has a body")
			}
			if tt.status == 200 {
				var response []map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if len(response) != 2 || response[0]["accrual"] != amount || response[0]["uploaded_at"] != date.Format(time.RFC3339) {
					t.Fatalf("unexpected response: %v", response)
				}
				if _, ok := response[1]["accrual"]; ok {
					t.Fatal("pending order exposes accrual")
				}
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	tokens := auth.NewTokenService("test-secret")
	token, err := tokens.Generate(42)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, header, cookie string
		status               int
	}{
		{name: "missing", status: 401},
		{name: "invalid", header: "Bearer bad-token", status: 401},
		{name: "wrong scheme", header: "Basic abc", status: 401},
		{name: "bearer", header: "Bearer " + token, status: 200},
		{name: "cookie", cookie: token, status: 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", tt.header)
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "token", Value: tt.cookie})
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				id, ok := UserIDFromContext(r.Context())
				if !ok || id != 42 {
					t.Fatal("missing authenticated user ID")
				}
				w.WriteHeader(200)
			})
			w := httptest.NewRecorder()
			Auth(tokens)(next).ServeHTTP(w, r)
			if w.Code != tt.status || called != (tt.status == 200) {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
		})
	}
	router := NewRouter(nil, nil, nil, tokens)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != 200 {
		t.Fatalf("ping status=%d", w.Code)
	}
}

func TestBalanceReadErrorsAndUnauthenticatedHandlers(t *testing.T) {
	repo := &balanceRepositoryStub{err: errors.New("database unavailable")}
	h := newBalanceHandler(service.NewBalanceService(repo))
	for _, action := range []http.HandlerFunc{h.balance, h.withdrawals} {
		w := httptest.NewRecorder()
		action(w, balanceTestRequest(http.MethodGet, "/", ""))
		if w.Code != 500 {
			t.Fatalf("status=%d want 500", w.Code)
		}
	}
	for _, action := range []http.HandlerFunc{h.balance, h.withdrawals, h.withdraw} {
		w := httptest.NewRecorder()
		action(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 401 {
			t.Fatalf("status=%d want 401", w.Code)
		}
	}
}
