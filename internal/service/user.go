// Package service — бизнес-логика приложения. Сервисы общаются
// с хранилищем через интерфейсы и ничего не знают про HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Vitaly898/diplom_1/internal/auth"
	"github.com/Vitaly898/diplom_1/internal/model"
	"github.com/Vitaly898/diplom_1/internal/repository"
)

// Бизнес-смыслы, которые хендлер мапит в HTTP-коды (409 / 401).
var (
	ErrInvalidLogin       = errors.New("логин должен содержать от 1 до 255 символов")
	ErrUserExists         = errors.New("логин уже занят")
	ErrInvalidCredentials = errors.New("неверная пара логин/пароль")
)

// Интерфейс объявлен ЗДЕСЬ, а не в repository: сервис диктует,
// что ему нужно от хранилища. В тестах подсунем мок.
type UserRepository interface {
	CreateUser(ctx context.Context, login, passwordHash string) (int64, error)
	GetUserByLogin(ctx context.Context, login string) (*model.User, error)
}

type UserService struct {
	repo   UserRepository
	hasher auth.PasswordHasher
	tokens *auth.TokenService
}

func NewUserService(repo UserRepository, hasher auth.PasswordHasher, tokens *auth.TokenService) *UserService {
	return &UserService{repo: repo, hasher: hasher, tokens: tokens}
}

// Register: хеш → создание в БД → токен (автоматическая аутентификация).
func (s *UserService) Register(ctx context.Context, login, password string) (string, error) {
	if login == "" || utf8.RuneCountInString(login) > model.MaxLoginLength {
		return "", ErrInvalidLogin
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return "", err
	}

	id, err := s.repo.CreateUser(ctx, login, hash)
	if errors.Is(err, repository.ErrLoginExists) {
		return "", ErrUserExists
	}
	if err != nil {
		return "", fmt.Errorf("регистрация: %w", err)
	}

	return s.tokens.Generate(id)
}

// Login: "не нашли" и "не совпал пароль" → ОДНА ошибка,
// иначе по ответам можно узнать существующие логины.
func (s *UserService) Login(ctx context.Context, login, password string) (string, error) {
	if login == "" || utf8.RuneCountInString(login) > model.MaxLoginLength {
		return "", ErrInvalidLogin
	}
	u, err := s.repo.GetUserByLogin(ctx, login)
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("аутентификация: %w", err)
	}

	if err := s.hasher.Check(u.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}

	return s.tokens.Generate(u.ID)
}
