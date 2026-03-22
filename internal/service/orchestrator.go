package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
	fraudpb "github.com/akylbek/payment-system/proto/fraud"
)

type Orchestrator struct {
	repo            interfaces.PaymentStateRepository
	redisClient     *redis.Client
	kafkaWriter     *kafka.Writer
	fraudGRPCClient fraudpb.FraudServiceClient
}

func NewOrchestrator(
	repo interfaces.PaymentStateRepository,
	redisClient *redis.Client,
	kafkaWriter *kafka.Writer,
	fraudGRPCClient fraudpb.FraudServiceClient,
) *Orchestrator {
	return &Orchestrator{
		repo:            repo,
		redisClient:     redisClient,
		kafkaWriter:     kafkaWriter,
		fraudGRPCClient: fraudGRPCClient,
	}
}

// ProcessPayment handles an incoming payment event
func (o *Orchestrator) ProcessPayment(ctx context.Context, event *models.PaymentEvent) error {
	telemetry.Logger.Info("Processing payment",
		zap.String("payment_id", event.PaymentID),
		zap.Float64("amount", event.Amount),
	)
	// Acquire lock
	lockKey := fmt.Sprintf("payment_lock:%s", event.PaymentID)
	locked := o.redisClient.SetNX(ctx, lockKey, "1", 30*time.Second)
	if !locked.Val() {
		return nil
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

	// Check fraud via gRPC
	fraudResp, err := o.fraudGRPCClient.CheckFraud(ctx, &fraudpb.CheckFraudRequest{
		PaymentId:  event.PaymentID,
		Amount:     event.Amount,
		CustomerId: event.CustomerID,
	})
	if err != nil {
		telemetry.Logger.Warn("Fraud check gRPC call failed",
			zap.String("payment_id", event.PaymentID),
			zap.Error(err),
		)
		if tErr := o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateFailed); tErr != nil {
			telemetry.Logger.Error("Failed to transition to FAILED", zap.Error(tErr))
		}
		return err
	}

	// Parallel: save fraud decision + state transitions
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Goroutine 1: save fraud decision
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := o.repo.UpdateFraudDecision(ctx, event.PaymentID, fraudResp.Decision); err != nil {
			telemetry.Logger.Error("Failed to update fraud decision",
				zap.String("payment_id", event.PaymentID),
				zap.Error(err),
			)
			errCh <- fmt.Errorf("failed to update fraud decision: %w", err)
		}
	}()

	// Goroutine 2: state transitions
	wg.Add(1)
	go func() {
		defer wg.Done()
		if fraudResp.Decision == "approve" {
			if err := o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateAuthorized); err != nil {
				errCh <- fmt.Errorf("failed to transition to AUTHORIZED: %w", err)
				return
			}
			if err := o.transitionState(ctx, event.PaymentID, models.StateAuthorized, models.StateCaptured); err != nil {
				errCh <- fmt.Errorf("failed to transition to CAPTURED: %w", err)
				return
			}
			if err := o.transitionState(ctx, event.PaymentID, models.StateCaptured, models.StateSucceeded); err != nil {
				errCh <- fmt.Errorf("failed to transition to SUCCEEDED: %w", err)
				return
			}
		} else {
			if err := o.transitionState(ctx, event.PaymentID, models.StateAuthPending, models.StateFailed); err != nil {
				errCh <- fmt.Errorf("failed to transition to FAILED: %w", err)
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

// transitionState uses TransitionStateWithOutbox — state update + outbox event in one transaction
func (o *Orchestrator) transitionState(ctx context.Context, paymentID string, from, to models.PaymentState) error {
	rows, err := o.repo.TransitionStateWithOutbox(ctx, paymentID, from, to)
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("invalid state transition from %s to %s for payment %s", from, to, paymentID)
	}

	telemetry.Logger.Info("Payment state transition",
		zap.String("payment_id", paymentID),
		zap.String("from_state", string(from)),
		zap.String("to_state", string(to)),
	)

	return nil
}

// RunOutboxPublisher polls the outbox table and publishes events to Kafka
func (o *Orchestrator) RunOutboxPublisher(ctx context.Context) {
	telemetry.Logger.Info("Starting outbox publisher")
	ticker := time.NewTicker(1000 * time.Millisecond) // 500ms instead of 500ms for higher throughput
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			telemetry.Logger.Info("Outbox publisher stopped")
			return
		case <-ticker.C:
			o.publishOutboxEvents(ctx)
		}
	}
}

func (o *Orchestrator) publishOutboxEvents(ctx context.Context) {
	events, err := o.repo.GetUnpublishedOutboxEvents(ctx, 1000)
	if err != nil {
		telemetry.Logger.Error("Failed to fetch outbox events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	// Build Kafka messages from outbox events
	messages := make([]kafka.Message, len(events))
	for i, event := range events {
		messages[i] = kafka.Message{
			Key:   []byte(event.AggregateID),
			Value: event.Payload,
		}
	}

	// Publish batch to Kafka
	if err := o.kafkaWriter.WriteMessages(ctx, messages...); err != nil {
		telemetry.Logger.Error("Failed to publish outbox events to Kafka",
			zap.Int("count", len(messages)),
			zap.Error(err),
		)
		return
	}

	// Mark each event as published
	for _, event := range events {
		if err := o.repo.MarkOutboxEventPublished(ctx, event.ID); err != nil {
			telemetry.Logger.Error("Failed to mark outbox event as published",
				zap.Int64("event_id", event.ID),
				zap.Error(err),
			)
		}
	}

	telemetry.Logger.Info("Published outbox events to Kafka",
		zap.Int("count", len(events)),
	)
}
