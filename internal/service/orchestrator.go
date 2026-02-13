package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
)

type Orchestrator struct {
	repo        interfaces.PaymentStateRepository
	redisClient *redis.Client
	nc          *nats.Conn
	kafkaWriter *kafka.Writer
}

func NewOrchestrator(
	repo interfaces.PaymentStateRepository,
	redisClient *redis.Client,
	nc *nats.Conn,
	kafkaWriter *kafka.Writer,
) *Orchestrator {
	return &Orchestrator{
		repo:        repo,
		redisClient: redisClient,
		nc:          nc,
		kafkaWriter: kafkaWriter,
	}
}

func (o *Orchestrator) ConsumePaymentEvents(kafkaBrokers string) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{kafkaBrokers},
		Topic:    "payment.created",
		GroupID:  "payment-orchestrator",
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	ctx := context.Background()

	telemetry.Logger.Info("Started consuming payment.created events")

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			telemetry.Logger.Error("Error reading message from Kafka", zap.Error(err))
			continue
		}

		var event models.PaymentEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			telemetry.Logger.Error("Error unmarshaling event", zap.Error(err))
			continue
		}

		telemetry.Logger.Info("Processing payment",
			zap.String("payment_id", event.PaymentID),
			zap.Float64("amount", event.Amount),
		)

		if err := o.processPayment(ctx, &event); err != nil {
			telemetry.Logger.Error("Error processing payment",
				zap.String("payment_id", event.PaymentID),
				zap.Error(err),
			)
		}
	}
}

func (o *Orchestrator) processPayment(ctx context.Context, event *models.PaymentEvent) error {
	// Acquire lock
	lockKey := fmt.Sprintf("payment_lock:%s", event.PaymentID)
	locked := o.redisClient.SetNX(ctx, lockKey, "1", 30*time.Second)
	if !locked.Val() {
		return fmt.Errorf("payment %s is already being processed", event.PaymentID)
	}
	defer o.redisClient.Del(ctx, lockKey)

	// Save initial state
	if err := o.repo.InsertInitialState(ctx, event.PaymentID, models.StateNew); err != nil {
		return err
	}

	// Transition to AUTH_PENDING
	if err := o.transitionState(ctx, event.PaymentID, models.StateNew, models.StateAuthPending); err != nil {
		return err
	}

	// Check fraud via NATS
	fraudReq := models.FraudCheckRequest{
		PaymentID:  event.PaymentID,
		Amount:     event.Amount,
		CustomerID: event.CustomerID,
	}
	fraudReqJSON, _ := json.Marshal(fraudReq)

	msg, err := o.nc.Request("fraud.check", fraudReqJSON, 5*time.Second)
	if err != nil {
		telemetry.Logger.Warn("Fraud check timeout",
			zap.String("payment_id", event.PaymentID),
			zap.Error(err),
		)
		o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateFailed)
		return err
	}

	var fraudResp models.FraudCheckResponse
	if err := json.Unmarshal(msg.Data, &fraudResp); err != nil {
		return err
	}

	// Save fraud decision
	o.repo.UpdateFraudDecision(ctx, event.PaymentID, fraudResp.Decision)

	if fraudResp.Decision == "approve" {
		o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateAuthorized)
		o.transitionState(ctx, event.PaymentID, models.StateAuthorized, models.StateCaptured)
		o.transitionState(ctx, event.PaymentID, models.StateCaptured, models.StateSucceeded)
	} else {
		o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateFailed)
	}

	return nil
}

func (o *Orchestrator) transitionState(ctx context.Context, paymentID string, from, to models.PaymentState) error {
	rows, err := o.repo.TransitionState(ctx, paymentID, from, to)
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("invalid state transition from %s to %s for payment %s", from, to, paymentID)
	}

	// Publish state change event
	stateEvent := map[string]interface{}{
		"payment_id":     paymentID,
		"state":          to,
		"previous_state": from,
		"timestamp":      time.Now(),
	}
	eventJSON, _ := json.Marshal(stateEvent)

	o.kafkaWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(paymentID),
		Value: eventJSON,
	})

	telemetry.Logger.Info("Payment state transition",
		zap.String("payment_id", paymentID),
		zap.String("from_state", string(from)),
		zap.String("to_state", string(to)),
	)

	return nil
}
