package model

import "time"

type Order struct {
	ID         int64
	UserID     int64
	Number     string
	Status     string
	Accrual    *float64
	UploadedAt time.Time
}
