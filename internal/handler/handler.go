// Package handler содержит HTTP-слой приложения: роутер, хендлеры
// и middleware. Здесь только приём/отдача HTTP — бизнес-логика живёт
// в слое service и вызывается отсюда.
package handler

import (
	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func NewRouter(userSvc userService, orderSvc orderService, balanceSvc balanceService, tokens *auth.TokenService) http.Handler {
	r := chi.NewRouter()
	r.Get("/ping", ping)
	ah := newAuthHandler(userSvc)
	r.Route("/api/user", func(r chi.Router) {
		r.Post("/register", ah.register)
		r.Post("/login", ah.login)

		r.Group(func(r chi.Router) {
			r.Use(Auth(tokens))
			oh := newOrderHandler(orderSvc)
			r.Post("/orders", oh.upload)
			r.Get("/orders", oh.list)
			bh := newBalanceHandler(balanceSvc)
			r.Get("/balance", bh.balance)
			r.Post("/balance/withdraw", bh.withdraw)
			r.Get("/withdrawals", bh.withdrawals)
		})
	})

	return r
}

func ping(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
