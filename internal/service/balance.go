package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/repository"
)

var ErrInvalidWithdrawalSum = errors.New("сумма списания должна быть положительной")
var ErrInsufficientFunds = errors.New("недостаточно баллов")

type BalanceRepository interface {
	GetBalance(ctx context.Context, userID int64) (model.Balance, error)
	GetWithdrawalsByUser(ctx context.Context, userID int64) ([]model.Withdrawal, error)
	Withdraw(ctx context.Context, userID int64, orderNumber string, sum model.Money) error
}

type BalanceService struct {
	repo BalanceRepository
}

func NewBalanceService(repo BalanceRepository) *BalanceService {
	return &BalanceService{repo: repo}
}

func (s *BalanceService) GetBalance(ctx context.Context, userID int64) (model.Balance, error) {
	balance, err := s.repo.GetBalance(ctx, userID)
	if err != nil {
		return model.Balance{}, fmt.Errorf("получение баланса пользователя: %w", err)
	}
	return balance, nil
}

func (s *BalanceService) ListWithdrawals(ctx context.Context, userID int64) ([]model.Withdrawal, error) {
	withdrawals, err := s.repo.GetWithdrawalsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("получение истории списаний: %w", err)
	}
	return withdrawals, nil
}

func (s *BalanceService) Withdraw(ctx context.Context, userID int64, orderNumber string, sum model.Money) error {
	if !luhnValid(orderNumber) {
		return ErrInvalidOrderNumber
	}
	if sum <= 0 {
		return ErrInvalidWithdrawalSum
	}

	err := s.repo.Withdraw(ctx, userID, orderNumber, sum)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return ErrInsufficientFunds
		}
		return fmt.Errorf("списание баллов: %w", err)
	}
	return nil
}
