package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/api"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/config"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/repository"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize telemetry
	if err := telemetry.InitTelemetry("payment-orchestrator"); err != nil {
		panic(fmt.Sprintf("Failed to initialize telemetry: %v", err))
	}
	defer telemetry.Shutdown(context.Background())

	telemetry.Logger.Info("Starting Payment Orchestrator")

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		telemetry.Logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize repository
	repo := repository.NewPaymentStateRepository(db)
	if err := repo.InitDB(); err != nil {
		telemetry.Logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURL,
	})

	// Connect to Kafka (for publishing state changes to ledger-service)
	kafkaWriter := &kafka.Writer{
		Addr:     kafka.TCP(cfg.KafkaBrokers),
		Topic:    "payment.state.changed",
		Balancer: &kafka.LeastBytes{},
	}
	defer kafkaWriter.Close()

	// Initialize orchestrator service
	orchestrator := service.NewOrchestrator(repo, redisClient, kafkaWriter, cfg.FraudServiceURL)

	// Start outbox publisher in background
	outboxCtx, outboxCancel := context.WithCancel(context.Background())
	defer outboxCancel()
	go orchestrator.RunOutboxPublisher(outboxCtx)

	// Setup router with all routes
	router := api.NewRouter(repo, orchestrator)

	// Setup HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Start server in goroutine
	go func() {
		telemetry.Logger.Info("Payment Orchestrator starting", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			telemetry.Logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	telemetry.Logger.Info("Shutting down server...")

	// Stop outbox publisher first
	outboxCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		telemetry.Logger.Error("Server forced to shutdown", zap.Error(err))
	}

	telemetry.Logger.Info("Server exited")
}
