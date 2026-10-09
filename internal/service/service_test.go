package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/repository"
)

type userRepoStub struct {
	create func(context.Context, string, string) (int64, error)
	get    func(context.Context, string) (*model.User, error)
}

func (r userRepoStub) CreateUser(ctx context.Context, login, hash string) (int64, error) {
	return r.create(ctx, login, hash)
}
func (r userRepoStub) GetUserByLogin(ctx context.Context, login string) (*model.User, error) {
	return r.get(ctx, login)
}

func TestUserRegister(t *testing.T) {
	hasher := auth.NewPasswordHasher()
	tokens := auth.NewTokenService("test-secret")
	dbErr := errors.New("database unavailable")
	for _, tt := range []struct {
		name, password   string
		repoErr, wantErr error
	}{
		{name: "success", password: "password"},
		{name: "duplicate", password: "password", repoErr: fmt.Errorf("wrapped: %w", repository.ErrLoginExists), wantErr: ErrUserExists},
		{name: "database failure", password: "password", repoErr: dbErr, wantErr: dbErr},
		{name: "hash failure", password: strings.Repeat("x", 73)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			repo := userRepoStub{create: func(_ context.Context, login, hash string) (int64, error) {
				called = true
				if login != "alice" || hasher.Check(hash, tt.password) != nil {
					t.Fatal("wrong login or password hash")
				}
				return 42, tt.repoErr
			}}
			svc := NewUserService(repo, *hasher, tokens)
			token, err := svc.Register(context.Background(), "alice", tt.password)
			if tt.name == "hash failure" {
				if err == nil || called {
					t.Fatal("hash failure did not prevent repository call")
				}
				return
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || token != "" {
					t.Fatalf("token=%q error=%v", token, err)
				}
				return
			}
			id, tokenErr := tokens.Validate(token)
			if err != nil || tokenErr != nil || id != 42 {
				t.Fatalf("id=%d error=%v tokenError=%v", id, err, tokenErr)
			}
		})
	}
}

func TestUserLogin(t *testing.T) {
	hasher := auth.NewPasswordHasher()
	hash, err := hasher.Hash("password")
	if err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokenService("test-secret")
	dbErr := errors.New("database unavailable")
	for _, tt := range []struct {
		name, password   string
		repoErr, wantErr error
	}{
		{name: "success", password: "password"},
		{name: "wrong password", password: "wrong", wantErr: ErrInvalidCredentials},
		{name: "unknown user", password: "password", repoErr: repository.ErrUserNotFound, wantErr: ErrInvalidCredentials},
		{name: "database failure", password: "password", repoErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := userRepoStub{get: func(_ context.Context, login string) (*model.User, error) {
				if login != "alice" {
					t.Fatal("wrong login")
				}
				return &model.User{ID: 42, PasswordHash: hash}, tt.repoErr
			}}
			token, err := NewUserService(repo, *hasher, tokens).Login(context.Background(), "alice", tt.password)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || token != "" {
					t.Fatalf("token=%q error=%v", token, err)
				}
				return
			}
			id, tokenErr := tokens.Validate(token)
			if err != nil || tokenErr != nil || id != 42 {
				t.Fatalf("id=%d error=%v tokenError=%v", id, err, tokenErr)
			}
		})
	}
}

type orderRepoStub struct {
	createErr, getErr, listErr error
	owner                      int64
	calls                      int
	orders                     []model.Order
}

func (r *orderRepoStub) CreateOrder(_ context.Context, userID int64, number string) error {
	r.calls++
	return r.createErr
}
func (r *orderRepoStub) GetOrderByNumber(context.Context, string) (*model.Order, error) {
	return &model.Order{UserID: r.owner}, r.getErr
}
func (r *orderRepoStub) GetOrdersByUser(context.Context, int64) ([]model.Order, error) {
	return r.orders, r.listErr
}

func TestLuhnValid(t *testing.T) {
	for _, tt := range []struct {
		number string
		valid  bool
	}{
		{"2377225624", true}, {"12345678903", true}, {"79927398713", true}, {"0018", true},
		{strings.Repeat("0", 64), true}, {strings.Repeat("0", 65), false},
		{"", false}, {"123", false}, {"79927398714", false}, {"12a3", false}, {"１２３", false}, {" 2377225624", false},
	} {
		if got := luhnValid(tt.number); got != tt.valid {
			t.Errorf("luhnValid(%q)=%v want %v", tt.number, got, tt.valid)
		}
	}
}

func TestUploadOrder(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tt := range []struct {
		name, number               string
		createErr, getErr, wantErr error
		owner                      int64
		already                    bool
	}{
		{name: "new", number: "2377225624"},
		{name: "invalid", number: "123", wantErr: ErrInvalidOrderNumber},
		{name: "too long", number: strings.Repeat("0", 65), wantErr: ErrInvalidOrderNumber},
		{name: "own duplicate", number: "2377225624", createErr: repository.ErrOrderExists, owner: 42, already: true},
		{name: "other user", number: "2377225624", createErr: repository.ErrOrderExists, owner: 43, wantErr: ErrOrderTaken},
		{name: "insert failure", number: "2377225624", createErr: dbErr, wantErr: dbErr},
		{name: "lookup failure", number: "2377225624", createErr: repository.ErrOrderExists, getErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &orderRepoStub{createErr: tt.createErr, getErr: tt.getErr, owner: tt.owner}
			already, err := NewOrderService(repo).UploadOrder(context.Background(), 42, tt.number)
			if already != tt.already || !errors.Is(err, tt.wantErr) {
				t.Fatalf("already=%v error=%v", already, err)
			}
			if tt.wantErr == ErrInvalidOrderNumber && repo.calls != 0 {
				t.Fatal("invalid order reached repository")
			}
		})
	}
}

func TestListOrders(t *testing.T) {
	repo := &orderRepoStub{orders: []model.Order{{Number: "2377225624"}}}
	got, err := NewOrderService(repo).ListOrders(context.Background(), 42)
	if err != nil || len(got) != 1 || got[0].Number != "2377225624" {
		t.Fatalf("orders=%v error=%v", got, err)
	}
	repo.listErr = errors.New("database unavailable")
	if _, err := NewOrderService(repo).ListOrders(context.Background(), 42); !errors.Is(err, repo.listErr) {
		t.Fatal(err)
	}
}

type balanceRepoStub struct {
	err   error
	calls int
}

func (r *balanceRepoStub) GetBalance(context.Context, int64) (model.Balance, error) {
	return model.Balance{Current: 1000, Withdrawn: 250}, r.err
}
func (r *balanceRepoStub) GetWithdrawalsByUser(context.Context, int64) ([]model.Withdrawal, error) {
	return []model.Withdrawal{{Sum: 250}}, r.err
}
func (r *balanceRepoStub) Withdraw(context.Context, int64, string, model.Money) error {
	r.calls++
	return r.err
}

func TestBalanceService(t *testing.T) {
	repo := &balanceRepoStub{}
	svc := NewBalanceService(repo)
	balance, err := svc.GetBalance(context.Background(), 42)
	if err != nil || balance.Current != 1000 || balance.Withdrawn != 250 {
		t.Fatalf("balance=%v error=%v", balance, err)
	}
	history, err := svc.ListWithdrawals(context.Background(), 42)
	if err != nil || len(history) != 1 || history[0].Sum != 250 {
		t.Fatalf("history=%v error=%v", history, err)
	}
	repo.err = errors.New("database unavailable")
	if _, err := svc.GetBalance(context.Background(), 42); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	if _, err := svc.ListWithdrawals(context.Background(), 42); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, number     string
		sum              model.Money
		repoErr, wantErr error
		calls            int
	}{
		{name: "success", number: "2377225624", sum: 100, calls: 1},
		{name: "invalid order", number: "123", sum: 100, wantErr: ErrInvalidOrderNumber},
		{name: "too long order", number: strings.Repeat("0", 65), sum: 100, wantErr: ErrInvalidOrderNumber},
		{name: "zero", number: "2377225624", wantErr: ErrInvalidWithdrawalSum},
		{name: "negative", number: "2377225624", sum: -1, wantErr: ErrInvalidWithdrawalSum},
		{name: "insufficient", number: "2377225624", sum: 100, repoErr: repository.ErrInsufficientFunds, wantErr: ErrInsufficientFunds, calls: 1},
		{name: "database failure", number: "2377225624", sum: 100, repoErr: repo.err, wantErr: repo.err, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &balanceRepoStub{err: tt.repoErr}
			err := NewBalanceService(repo).Withdraw(context.Background(), 42, tt.number, tt.sum)
			if !errors.Is(err, tt.wantErr) || repo.calls != tt.calls {
				t.Fatalf("error=%v calls=%d", err, repo.calls)
			}
		})
	}
}

func TestLoginLengthValidation(t *testing.T) {
	hasher := auth.NewPasswordHasher()
	tokens := auth.NewTokenService("test-secret")
	repo := userRepoStub{
		create: func(context.Context, string, string) (int64, error) {
			t.Fatal("invalid login reached repository")
			return 0, nil
		},
		get: func(context.Context, string) (*model.User, error) {
			t.Fatal("invalid login reached repository")
			return nil, nil
		},
	}
	svc := NewUserService(repo, *hasher, tokens)
	for _, login := range []string{"", strings.Repeat("я", model.MaxLoginLength+1)} {
		if _, err := svc.Register(context.Background(), login, "password"); !errors.Is(err, ErrInvalidLogin) {
			t.Fatal(err)
		}
		if _, err := svc.Login(context.Background(), login, "password"); !errors.Is(err, ErrInvalidLogin) {
			t.Fatal(err)
		}
	}
	boundary := strings.Repeat("я", model.MaxLoginLength)
	repo.get = func(_ context.Context, login string) (*model.User, error) {
		if login != boundary {
			t.Fatal("login changed")
		}
		return nil, repository.ErrUserNotFound
	}
	if _, err := NewUserService(repo, *hasher, tokens).Login(context.Background(), boundary, "password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
}
