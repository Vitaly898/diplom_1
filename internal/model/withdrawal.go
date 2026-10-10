package model

import "time"

type Withdrawal struct {
	ID          int64
	UserID      int64
	OrderNumber string
	Sum         Money
	ProcessedAt time.Time
}
