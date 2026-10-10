package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("недействительный или просроченный токен")

const tokenTTL = 24 * time.Hour

type TokenService struct {
	secret []byte
}

type claims struct {
	UserID int64 `json:"uid"`
	jwt.RegisteredClaims
}

func NewTokenService(secret string) *TokenService {
	return &TokenService{secret: []byte(secret)}
}

func (s *TokenService) Generate(userID int64) (string, error) {
	c := claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

func (s *TokenService) Validate(tokenString string) (int64, error) {
	var c claims

	token, err := jwt.ParseWithClaims(tokenString, &c, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("неожиданный метод подписи: %v", tok.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}

	return c.UserID, nil
}
