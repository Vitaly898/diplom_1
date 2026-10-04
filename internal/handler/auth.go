package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitaly898/diplom_1/internal/service"
)

// Тело register/login — одинаковое по заданию.
type authRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}

// Локальный контракт: хендлер диктует, что нужно от сервиса.
type userService interface {
	Register(ctx context.Context, login, password string) (string, error)
	Login(ctx context.Context, login, password string) (string, error)
}

type authHandler struct {
	svc userService
}

func newAuthHandler(svc userService) *authHandler {
	return &authHandler{svc: svc}
}

func (h *authHandler) register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}
	if req.Login == "" || req.Password == "" {
		http.Error(w, "логин и пароль обязательны", http.StatusBadRequest)
		return
	}

	token, err := h.svc.Register(r.Context(), req.Login, req.Password)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	writeToken(w, token)
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}
	if req.Login == "" || req.Password == "" {
		http.Error(w, "логин и пароль обязательны", http.StatusBadRequest)
		return
	}

	token, err := h.svc.Login(r.Context(), req.Login, req.Password)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	writeToken(w, token)
}

// Единственное место перевода ошибок сервиса → HTTP-коды.
func (h *authHandler) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrUserExists):
		http.Error(w, "логин уже занят", http.StatusConflict)
	case errors.Is(err, service.ErrInvalidCredentials):
		http.Error(w, "неверная пара логин/пароль", http.StatusUnauthorized)
	default:
		http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
	}
}

// Токен и в теле (JSON), и в httpOnly-куке (JS не прочитает — защита от XSS).
func writeToken(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(authResponse{Token: token})
}
