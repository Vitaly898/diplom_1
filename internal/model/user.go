package model

import "time"

type User struct {
	ID           int64
	Login        string
	PasswordHash string
	CreatedAt    time.Time
}
