package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestConnectInvalidDSN(t *testing.T) {
	if db, err := Connect(context.Background(), "://invalid"); err == nil {
		db.Close()
		t.Fatal("invalid DSN accepted")
	}
}

func TestPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URI")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URI to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("gophermart_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Start with the schema deployed before review, then upgrade populated tables.
	migrationDB := stdlib.OpenDBFromPool(db)
	defer migrationDB.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrationsFS)
	if err := goose.UpToContext(ctx, migrationDB, "migrations", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO users (login, password_hash) VALUES ('legacy', 'legacy-hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO orders (user_id, number) SELECT id, 'legacy-order' FROM users WHERE login = 'legacy'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("repeated migration: %v", err)
	}
	t.Run("schema constraints", func(t *testing.T) {
		var status string
		if err := db.QueryRow(ctx, `SELECT status::text FROM orders WHERE number = 'legacy-order'`).Scan(&status); err != nil || status != "NEW" {
			t.Fatalf("legacy status=%s err=%v", status, err)
		}
		for _, tc := range []struct {
			query string
			args  []any
			code  string
		}{
			{`INSERT INTO users (login, password_hash) VALUES ($1, 'hash')`, []any{strings.Repeat("x", 256)}, "22001"},
			{`INSERT INTO users (login, password_hash) VALUES ('long-hash', $1)`, []any{strings.Repeat("x", 61)}, "22001"},
			{`INSERT INTO orders (user_id, number) SELECT id, $1 FROM users WHERE login = 'legacy'`, []any{strings.Repeat("0", 65)}, "22001"},
			{`INSERT INTO withdrawals (user_id, order_number, sum) SELECT id, $1, 1 FROM users WHERE login = 'legacy'`, []any{strings.Repeat("0", 65)}, "22001"},
			{`UPDATE orders SET status = 'UNKNOWN' WHERE number = 'legacy-order'`, nil, "22P02"},
		} {
			_, err := db.Exec(ctx, tc.query, tc.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("query=%s err=%v", tc.query, err)
			}
		}
		if _, err := db.Exec(ctx, `DELETE FROM orders WHERE number = 'legacy-order'`); err != nil {
			t.Fatal(err)
		}
	})
	users := NewUserRepository(db)
	orders := NewOrderRepository(db)
	balances := NewBalanceRepository(db)
	userID, err := users.CreateUser(ctx, "alice", "password-hash")
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := users.CreateUser(ctx, "bob", "other-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "alice", "duplicate"); !errors.Is(err, ErrLoginExists) {
		t.Fatalf("duplicate login: %v", err)
	}
	user, err := users.GetUserByLogin(ctx, "alice")
	if err != nil || user.ID != userID || user.PasswordHash != "password-hash" || user.CreatedAt.IsZero() {
		t.Fatalf("user=%v error=%v", user, err)
	}
	if _, err := users.GetUserByLogin(ctx, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	balance, err := balances.GetBalance(ctx, userID)
	if err != nil || balance != (model.Balance{}) {
		t.Fatalf("initial balance=%v error=%v", balance, err)
	}
	history, err := balances.GetWithdrawalsByUser(ctx, userID)
	if err != nil || len(history) != 0 {
		t.Fatalf("initial history=%v error=%v", history, err)
	}
	for _, number := range []string{"2377225624", "12345678903"} {
		if err := orders.CreateOrder(ctx, userID, number); err != nil {
			t.Fatal(err)
		}
	}
	if err := orders.CreateOrder(ctx, otherID, "2377225624"); !errors.Is(err, ErrOrderExists) {
		t.Fatalf("duplicate order: %v", err)
	}
	if _, err := orders.GetOrderByNumber(ctx, "missing"); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("missing order: %v", err)
	}
	order, err := orders.GetOrderByNumber(ctx, "2377225624")
	if err != nil || order.UserID != userID || order.Status != "NEW" || order.Accrual != nil || order.UploadedAt.IsZero() {
		t.Fatalf("order=%v error=%v", order, err)
	}
	list, err := orders.GetOrdersByUser(ctx, userID)
	if err != nil || len(list) != 2 {
		t.Fatalf("orders=%v error=%v", list, err)
	}
	pending, err := orders.GetPendingOrderNumbers(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v error=%v", pending, err)
	}
	if err := orders.UpdateOrderAccrual(ctx, "2377225624", "PROCESSING", nil); err != nil {
		t.Fatal(err)
	}
	amount := model.Money(1025)
	if err := orders.UpdateOrderAccrual(ctx, "2377225624", "PROCESSED", &amount); err != nil {
		t.Fatal(err)
	}
	changed := model.Money(9999)
	if err := orders.UpdateOrderAccrual(ctx, "2377225624", "PROCESSED", &changed); err != nil {
		t.Fatal(err)
	}
	if err := orders.UpdateOrderAccrual(ctx, "2377225624", "PROCESSING", nil); err != nil {
		t.Fatal(err)
	}
	if err := orders.UpdateOrderAccrual(ctx, "12345678903", "INVALID", nil); err != nil {
		t.Fatal(err)
	}
	if err := orders.UpdateOrderAccrual(ctx, "12345678903", "PROCESSED", &changed); err != nil {
		t.Fatal(err)
	}
	pending, err = orders.GetPendingOrderNumbers(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("final orders still pending: %v error=%v", pending, err)
	}
	order, err = orders.GetOrderByNumber(ctx, "2377225624")
	if err != nil || order.Status != "PROCESSED" || order.Accrual == nil || *order.Accrual != 10.25 {
		t.Fatalf("final order overwritten: %v error=%v", order, err)
	}
	balance, err = balances.GetBalance(ctx, userID)
	if err != nil || balance.Current != 1025 || balance.Withdrawn != 0 {
		t.Fatalf("credited balance=%v error=%v", balance, err)
	}
	if err := balances.Withdraw(ctx, userID, "future-order", 1026); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("overdraft: %v", err)
	}
	if err := balances.Withdraw(ctx, userID, "future-order", 0); err == nil {
		t.Fatal("zero withdrawal accepted")
	}
	if err := balances.Withdraw(ctx, userID, "future-order", 1025); err != nil {
		t.Fatal(err)
	}
	balance, err = balances.GetBalance(ctx, userID)
	if err != nil || balance.Current != 0 || balance.Withdrawn != 1025 {
		t.Fatalf("withdrawn balance=%v error=%v", balance, err)
	}
	history, err = balances.GetWithdrawalsByUser(ctx, userID)
	if err != nil || len(history) != 1 || history[0].Sum != 1025 || history[0].OrderNumber != "future-order" || history[0].ProcessedAt.IsZero() {
		t.Fatalf("history=%v error=%v", history, err)
	}
	balance, err = balances.GetBalance(ctx, otherID)
	if err != nil || balance != (model.Balance{}) {
		t.Fatalf("other user's balance changed: %v error=%v", balance, err)
	}

	t.Run("parallel withdrawals", func(t *testing.T) {
		if err := orders.CreateOrder(ctx, otherID, "79927398713"); err != nil {
			t.Fatal(err)
		}
		amount := model.Money(10000)
		if err := orders.UpdateOrderAccrual(ctx, "79927398713", "PROCESSED", &amount); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, number := range []string{"first-withdrawal", "second-withdrawal"} {
			go func(number string) { <-start; results <- balances.Withdraw(ctx, otherID, number, 8000) }(number)
		}
		close(start)
		success, insufficient := 0, 0
		for i := 0; i < 2; i++ {
			err := <-results
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrInsufficientFunds):
				insufficient++
			default:
				t.Errorf("withdrawal: %v", err)
			}
		}
		if success != 1 || insufficient != 1 {
			t.Fatalf("success=%d insufficient=%d", success, insufficient)
		}
		balance, err := balances.GetBalance(ctx, otherID)
		if err != nil || balance.Current != 2000 || balance.Withdrawn != 8000 {
			t.Fatalf("concurrent balance=%v error=%v", balance, err)
		}
		if err := balances.Withdraw(ctx, otherID, "last-withdrawal", 2000); err != nil {
			t.Fatal(err)
		}
		history, err := balances.GetWithdrawalsByUser(ctx, otherID)
		if err != nil || len(history) != 2 || history[0].Sum != 2000 || history[1].Sum != 8000 {
			t.Fatalf("history order=%v error=%v", history, err)
		}
	})

	t.Run("cancelled transaction", func(t *testing.T) {
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if err := balances.Withdraw(cancelled, userID, "cancelled", 1); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled transaction: %v", err)
		}
	})
}
