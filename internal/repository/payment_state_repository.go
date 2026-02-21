package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

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
		`CREATE TABLE IF NOT EXISTS outbox_events (
			id BIGSERIAL PRIMARY KEY,
			aggregate_id VARCHAR(255) NOT NULL,
			event_type VARCHAR(100) NOT NULL,
			payload JSONB NOT NULL,
			published BOOLEAN DEFAULT FALSE,
			published_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished ON outbox_events(created_at) WHERE published = FALSE`,
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

// TransitionStateWithOutbox updates payment state and inserts an outbox event in a single transaction
func (r *PaymentStateRepository) TransitionStateWithOutbox(ctx context.Context, paymentID string, from, to models.PaymentState) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 1. Update state
	result, err := tx.ExecContext(ctx, `
		UPDATE payment_states 
		SET state = $1, previous_state = $2, updated_at = NOW()
		WHERE payment_id = $3 AND state = $4
	`, to, from, paymentID, from)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rows == 0 {
		return 0, nil
	}

	// 2. Insert outbox event (same transaction)
	payload, err := json.Marshal(map[string]interface{}{
		"payment_id":     paymentID,
		"state":          to,
		"previous_state": from,
		"timestamp":      time.Now(),
	})
	if err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events (aggregate_id, event_type, payload)
		VALUES ($1, $2, $3)
	`, paymentID, "payment.state.changed", payload)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return rows, nil
}

func (r *PaymentStateRepository) UpdateFraudDecision(ctx context.Context, paymentID, decision string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE payment_states SET fraud_decision = $1 WHERE payment_id = $2`, decision, paymentID)
	return err
}

func (r *PaymentStateRepository) GetByPaymentID(ctx context.Context, paymentID string) (*models.PaymentStateInfo, error) {
	var info models.PaymentStateInfo
	err := r.db.QueryRowContext(ctx, `
		SELECT state, COALESCE(previous_state, ''), COALESCE(fraud_decision, ''), created_at, updated_at
		FROM payment_states WHERE payment_id = $1
	`, paymentID).Scan(&info.State, &info.PreviousState, &info.FraudDecision, &info.CreatedAt, &info.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// GetUnpublishedOutboxEvents returns unpublished outbox events
func (r *PaymentStateRepository) GetUnpublishedOutboxEvents(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, aggregate_id, event_type, payload
		FROM outbox_events
		WHERE published = FALSE
		ORDER BY created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.OutboxEvent
	for rows.Next() {
		var e models.OutboxEvent
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// MarkOutboxEventPublished marks an outbox event as published
func (r *PaymentStateRepository) MarkOutboxEventPublished(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox_events SET published = TRUE, published_at = NOW()
		WHERE id = $1
	`, id)
	return err
}
