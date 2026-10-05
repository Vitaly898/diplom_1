package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHasher(t *testing.T) {
	hasher := NewPasswordHasher()
	hash, err := hasher.Hash("secret")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "secret" {
		t.Fatal("password was stored without hashing")
	}
	if err := hasher.Check(hash, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := hasher.Check(hash, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err := hasher.Check("invalid-hash", "secret"); err == nil {
		t.Fatal("invalid hash accepted")
	}
	if _, err := hasher.Hash(strings.Repeat("x", 73)); err == nil {
		t.Fatal("oversized bcrypt password accepted")
	}
}

func TestTokenService(t *testing.T) {
	svc := NewTokenService("secret")
	token, err := svc.Generate(42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.Validate(token)
	if err != nil || id != 42 {
		t.Fatalf("id=%d error=%v", id, err)
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID:           42,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))},
	}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims{UserID: 42}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "garbage", expired, unsigned} {
		if _, err := svc.Validate(input); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("invalid token accepted: %v", err)
		}
	}
	if _, err := NewTokenService("different-secret").Validate(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("wrong signing secret accepted")
	}
}
