package interfaces

import (
	"context"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
)

// PaymentStateRepository defines the contract for payment state data access
type PaymentStateRepository interface {
	InsertInitialState(ctx context.Context, paymentID string, state models.PaymentState) error
	TransitionState(ctx context.Context, paymentID string, from, to models.PaymentState) (int64, error)
	UpdateFraudDecision(ctx context.Context, paymentID, decision string) error
	GetByPaymentID(ctx context.Context, paymentID string) (*models.PaymentStateInfo, error)
}
