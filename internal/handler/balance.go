package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/service"
)

type balanceService interface {
	GetBalance(ctx context.Context, userID int64) (model.Balance, error)
	ListWithdrawals(ctx context.Context, userID int64) ([]model.Withdrawal, error)
	Withdraw(ctx context.Context, userID int64, orderNumber string, sum model.Money) error
}

type balanceHandler struct {
	svc balanceService
}

func newBalanceHandler(svc balanceService) *balanceHandler {
	return &balanceHandler{svc: svc}
}

type balanceResponse struct {
	Current   json.Number `json:"current"`
	Withdrawn json.Number `json:"withdrawn"`
}

type withdrawalRequest struct {
	Order string          `json:"order"`
	Sum   json.RawMessage `json:"sum"`
}

type withdrawalResponse struct {
	Order       string      `json:"order"`
	Sum         json.Number `json:"sum"`
	ProcessedAt string      `json:"processed_at"`
}

func (h *balanceHandler) balance(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
		return
	}

	balance, err := h.svc.GetBalance(r.Context(), userID)
	if err != nil {
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	response := balanceResponse{
		Current:   json.Number(balance.Current.String()),
		Withdrawn: json.Number(balance.Withdrawn.String()),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *balanceHandler) withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
		return
	}

	var request withdrawalRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}

	sum, err := model.ParseMoney(string(request.Sum))
	if err != nil {
		http.Error(w, "неверный формат суммы", http.StatusBadRequest)
		return
	}

	if err := h.svc.Withdraw(r.Context(), userID, request.Order, sum); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidOrderNumber):
			http.Error(w, "неверный номер заказа", http.StatusUnprocessableEntity)
		case errors.Is(err, service.ErrInvalidWithdrawalSum):
			http.Error(w, "сумма списания должна быть положительной", http.StatusBadRequest)
		case errors.Is(err, service.ErrInsufficientFunds):
			http.Error(w, "недостаточно баллов", http.StatusPaymentRequired)
		default:
			http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *balanceHandler) withdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
		return
	}

	withdrawals, err := h.svc.ListWithdrawals(r.Context(), userID)
	if err != nil {
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	response := make([]withdrawalResponse, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		response = append(response, withdrawalResponse{
			Order:       withdrawal.OrderNumber,
			Sum:         json.Number(withdrawal.Sum.String()),
			ProcessedAt: withdrawal.ProcessedAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
