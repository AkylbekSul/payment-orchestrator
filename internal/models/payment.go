package models

import "time"

type PaymentState string

const (
	StateNew         PaymentState = "NEW"
	StateAuthPending PaymentState = "AUTH_PENDING"
	StateAuthorized  PaymentState = "AUTHORIZED"
	StateCaptured    PaymentState = "CAPTURED"
	StateSucceeded   PaymentState = "SUCCEEDED"
	StateFailed      PaymentState = "FAILED"
	StateCanceled    PaymentState = "CANCELED"
)

type PaymentEvent struct {
	PaymentID  string    `json:"payment_id"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency"`
	CustomerID string    `json:"customer_id"`
	MerchantID string    `json:"merchant_id"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type FraudCheckRequest struct {
	PaymentID  string  `json:"payment_id"`
	Amount     float64 `json:"amount"`
	CustomerID string  `json:"customer_id"`
}

type FraudCheckResponse struct {
	Decision string `json:"decision"` // approve, deny, manual_review
	Reason   string `json:"reason"`
}

// PaymentStateInfo represents the current state info of a payment
type PaymentStateInfo struct {
	State         string
	PreviousState string
	FraudDecision string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
