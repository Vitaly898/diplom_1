package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Vitaly898/diplom_1/internal/auth"
)

// Типизированный ключ вместо голого string: тип виден только этому
// пакету, коллизии ключей с чужими пакетами исключены.
type ctxKey string

const userIDKey ctxKey = "userID"

// Auth — middleware: проверяет JWT, кладёт userID в контекст.
// Сигнатура func(http.Handler) http.Handler — стандартный паттерн.
func Auth(tokens *auth.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := tokenFromRequest(r)
			if tokenString == "" {
				http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
				return
			}

			userID, err := tokens.Validate(tokenString)
			if err != nil {
				http.Error(w, "пользователь не аутентифицирован", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// tokenFromRequest достаёт токен из куки или заголовка Authorization: Bearer ...
func tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie("token"); err == nil {
		return c.Value
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// UserIDFromContext извлекает userID, положенный middleware Auth.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}
