package repository

import (
	"context"
	"database/sql"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
)

type PaymentStateRepository struct {
	db *sql.DB
}

func NewPaymentStateRepository(db *sql.DB) *PaymentStateRepository {
	return &PaymentStateRepository{db: db}
}

func (r *PaymentStateRepository) InitDB() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS payment_states (
			payment_id VARCHAR(255) PRIMARY KEY,
			state VARCHAR(50) NOT NULL,
			previous_state VARCHAR(50),
			fraud_decision VARCHAR(50),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_states_state ON payment_states(state)`,
	}

	for _, query := range queries {
		if _, err := r.db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

func (r *PaymentStateRepository) InsertInitialState(ctx context.Context, paymentID string, state models.PaymentState) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO payment_states (payment_id, state, previous_state)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_id) DO NOTHING
	`, paymentID, state, "")
	return err
}

func (r *PaymentStateRepository) TransitionState(ctx context.Context, paymentID string, from, to models.PaymentState) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE payment_states 
		SET state = $1, previous_state = $2, updated_at = NOW()
		WHERE payment_id = $3 AND state = $4
	`, to, from, paymentID, from)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *PaymentStateRepository) UpdateFraudDecision(ctx context.Context, paymentID, decision string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE payment_states SET fraud_decision = $1 WHERE payment_id = $2`, decision, paymentID)
	return err
}

func (r *PaymentStateRepository) GetByPaymentID(ctx context.Context, paymentID string) (*models.PaymentStateInfo, error) {
	var info models.PaymentStateInfo
	err := r.db.QueryRowContext(ctx, `
		SELECT state, previous_state, fraud_decision, created_at, updated_at
		FROM payment_states WHERE payment_id = $1
	`, paymentID).Scan(&info.State, &info.PreviousState, &info.FraudDecision, &info.CreatedAt, &info.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &info, nil
}
