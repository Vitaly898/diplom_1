package model

import "time"

// Статусы заказа по заданию.
const (
	StatusNew        = "NEW"
	StatusProcessing = "PROCESSING"
	StatusInvalid    = "INVALID"
	StatusProcessed  = "PROCESSED"
)

type Order struct {
	ID         int64
	UserID     int64
	Number     string
	Status     string
	Accrual    *float64
	UploadedAt time.Time
}
