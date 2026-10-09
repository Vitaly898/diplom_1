package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/repository"
)

var (
	ErrInvalidOrderNumber = errors.New("неверный формат номера заказа")
	ErrOrderTaken         = errors.New("номер заказа уже загружен другим пользователем")
)

// Статусы заказа по заданию.
const (
	StatusNew        = "NEW"
	StatusProcessing = "PROCESSING"
	StatusInvalid    = "INVALID"
	StatusProcessed  = "PROCESSED"
)

// Интерфейс объявлен в сервисе (инверсия зависимости):
// сервис диктует хранилищу, какой контракт ему нужен.
type OrderRepository interface {
	CreateOrder(ctx context.Context, userID int64, number string) error
	GetOrderByNumber(ctx context.Context, number string) (*model.Order, error)
	GetOrdersByUser(ctx context.Context, userID int64) ([]model.Order, error)
}

type OrderService struct {
	repo OrderRepository
}

func NewOrderService(repo OrderRepository) *OrderService {
	return &OrderService{repo: repo}
}

// UploadOrder сохраняет номер заказа.
// already == true — номер уже был загружен ЭТИМ пользователем (HTTP 200).
func (s *OrderService) UploadOrder(ctx context.Context, userID int64, number string) (already bool, err error) {
	if !luhnValid(number) {
		return false, ErrInvalidOrderNumber
	}

	err = s.repo.CreateOrder(ctx, userID, number)
	// Исправлена инвертированная логика (см. разбор ниже):
	// 1) вставка прошла → новый заказ (202);
	// 2) ошибка НЕ про дубликат → настоящий сбой, оборачиваем;
	// 3) ошибка про дубликат → смотрим владельца (200 или 409).
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, repository.ErrOrderExists) {
		return false, fmt.Errorf("загрузка заказа: %w", err)
	}

	o, err := s.repo.GetOrderByNumber(ctx, number)
	if err != nil {
		return false, fmt.Errorf("проверка владельца заказа: %w", err)
	}
	if o.UserID == userID {
		return true, nil
	}
	return false, ErrOrderTaken
}

// Исправлено: метода не было вообще — хендлер не компилировался бы.
func (s *OrderService) ListOrders(ctx context.Context, userID int64) ([]model.Order, error) {
	return s.repo.GetOrdersByUser(ctx, userID)
}

// luhnValid — проверка контрольной цифры по алгоритму Луна.
// Идём справа налево от контрольной цифры; каждую вторую удваиваем;
// двузначные результаты уменьшаем на 9; сумма кратна 10.
func luhnValid(number string) bool {
	if number == "" || len(number) > model.MaxOrderNumberLength {
		return false
	}
	sum := 0
	double := false // удваивать начинаем со второй цифры с конца
	for i := len(number) - 1; i >= 0; i-- {
		c := number[i]
		if c < '0' || c > '9' {
			return false // любой нецифровой символ — брак номера
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
