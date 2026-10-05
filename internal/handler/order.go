package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/service"
)

type orderService interface {
	UploadOrder(ctx context.Context, userID int64, number string) (bool, error)
	ListOrders(ctx context.Context, userID int64) ([]model.Order, error)
}

type orderHandler struct {
	svc orderService
}

func newOrderHandler(svc orderService) *orderHandler {
	return &orderHandler{svc: svc}
}

type orderResponse struct {
	Number     string   `json:"number"`
	Status     string   `json:"status"`
	Accrual    *float64 `json:"accrual,omitempty"`
	UploadedAt string   `json:"uploaded_at"` // формат RFC3339 по заданию
}

func (h *orderHandler) upload(w http.ResponseWriter, r *http.Request) {

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}
	number := strings.TrimSpace(string(body))

	already, err := h.svc.UploadOrder(r.Context(), userID, number)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidOrderNumber):
			http.Error(w, "неверный формат номера заказа", http.StatusUnprocessableEntity)
		case errors.Is(err, service.ErrOrderTaken):
			http.Error(w, "номер заказа уже загружен другим пользователем", http.StatusConflict)
		default:
			http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		}
		return
	}

	if already {
		w.WriteHeader(http.StatusOK) // уже загружал этот номер
		return
	}
	w.WriteHeader(http.StatusAccepted) // новый номер принят в обработку
}

// list — GET /api/user/orders.
func (h *orderHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
		return
	}

	orders, err := h.svc.ListOrders(r.Context(), userID)
	if err != nil {
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent) // 204 — без тела
		return
	}

	resp := make([]orderResponse, 0, len(orders))
	for _, o := range orders {
		resp = append(resp, orderResponse{
			Number:     o.Number,
			Status:     o.Status,
			Accrual:    o.Accrual,
			UploadedAt: o.UploadedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
