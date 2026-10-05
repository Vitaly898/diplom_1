// Gophermart — накопительная система лояльности.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vitaly898/diplom_1/internal/accrual"
	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/Vitaly898/diplom_1/internal/config"
	"github.com/Vitaly898/diplom_1/internal/handler"
	"github.com/Vitaly898/diplom_1/internal/repository"
	"github.com/Vitaly898/diplom_1/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Printf("ошибка: %v", err)
		os.Exit(1)
	}
}

// run содержит всю логику запуска приложения.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("загрузка конфигурации: %w", err)
	}

	// Контекст, который отменится при получении SIGINT (Ctrl+C) или SIGTERM.
	// Создаём его ДО подключения к БД: если сервис попросили остановить
	// ещё на старте — Ping не будет ждать вхолостую, а выйдет сразу.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Подключение к БД. Не удалось — падаем (fail fast): без БД
	// сервис бессмыслен.
	db, err := repository.Connect(ctx, cfg.DatabaseURI)
	if err != nil {
		return fmt.Errorf("база данных: %w", err)
	}
	defer db.Close()

	// 2. Приводим схему БД к актуальному состоянию. Идемпотентно.
	if err := repository.Migrate(db); err != nil {
		return fmt.Errorf("миграции: %w", err)
	}

	// 3. Dependency injection: hasher → repository → service → handler.
	// main — единственное место, знающее всю схему подключения слоёв.
	hasher := auth.NewPasswordHasher()
	tokens := auth.NewTokenService(cfg.TokenSecret)
	userRepo := repository.NewUserRepository(db)
	userSvc := service.NewUserService(userRepo, *hasher, tokens)
	orderRepo := repository.NewOrderRepository(db)
	orderSvc := service.NewOrderService(orderRepo)
	balanceRepo := repository.NewBalanceRepository(db)
	balanceSvc := service.NewBalanceService(balanceRepo)
	accrualClient, err := accrual.NewClient(cfg.AccrualSystemAddress)
	if err != nil {
		return fmt.Errorf("создание клиента начислений: %w", err)
	}
	worker := accrual.NewWorker(orderRepo, accrualClient, slog.Default())
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(workerCtx)
	}()
	defer func() {
		cancelWorker()
		<-workerDone
	}()

	// 4. HTTP-сервер.
	srv := &http.Server{
		Addr:    cfg.RunAddress,
		Handler: handler.NewRouter(userSvc, orderSvc, balanceSvc, tokens),
	}

	// 5. Запуск в горутине: ListenAndServe блокирующий.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("сервер запущен на %s", cfg.RunAddress)
		errCh <- srv.ListenAndServe()
	}()

	// 6. Ждём: либо сервер упал сам, либо пришёл сигнал от ОС.
	select {
	case err := <-errCh:
		// ErrServerClosed — штатное завершение через Shutdown(), не сбой.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("сервер: %w", err)
	case <-ctx.Done():
		log.Println("получен сигнал остановки, завершаем работу...")
	}

	// 7. Graceful shutdown: не дольше 10 секунд. Сигнальный ctx уже
	// отменён — для Shutdown создаём свежий контекст.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("остановка сервера: %w", err)
	}

	log.Println("сервер остановлен")
	return nil
}
