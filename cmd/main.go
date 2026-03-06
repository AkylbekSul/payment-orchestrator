package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/api"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/config"
	grpcserver "github.com/akylbek/payment-system/payment-orchestrator/internal/grpcserver"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/repository"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
	fraudpb "github.com/akylbek/payment-system/proto/fraud"
	paymentpb "github.com/akylbek/payment-system/proto/payment"
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

	// Configure connection pool for high concurrency
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Initialize repository
	repo := repository.NewPaymentStateRepository(db)
	if err := repo.InitDB(); err != nil {
		telemetry.Logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		PoolSize: 100,
	})

	// Connect to Kafka (for publishing state changes to ledger-service)
	kafkaWriter := &kafka.Writer{
		Addr:     kafka.TCP(cfg.KafkaBrokers),
		Topic:    "payment.state.changed",
		Balancer: &kafka.LeastBytes{},
	}
	defer kafkaWriter.Close()

	// Connect to Fraud Service via gRPC
	fraudConn, err := grpc.NewClient(cfg.FraudServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		telemetry.Logger.Fatal("Failed to connect to fraud service via gRPC", zap.Error(err))
	}
	defer fraudConn.Close()
	fraudClient := fraudpb.NewFraudServiceClient(fraudConn)

	// Initialize orchestrator service with gRPC fraud client
	orchestrator := service.NewOrchestrator(repo, redisClient, kafkaWriter, fraudClient)

	// Start outbox publisher in background
	// outboxCtx, outboxCancel := context.WithCancel(context.Background())
	// defer outboxCancel()
	// go orchestrator.RunOutboxPublisher(outboxCtx)

	// Setup HTTP router (health, metrics, legacy HTTP endpoints)
	router := api.NewRouter(repo, orchestrator)

	// Setup HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Start HTTP server in goroutine
	go func() {
		telemetry.Logger.Info("Payment Orchestrator HTTP starting", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			telemetry.Logger.Fatal("Failed to start HTTP server", zap.Error(err))
		}
	}()

	// Start gRPC server
	grpcSrv := grpc.NewServer()
	paymentGRPCServer := grpcserver.NewPaymentGRPCServer(repo, orchestrator)
	paymentpb.RegisterPaymentOrchestratorServer(grpcSrv, paymentGRPCServer)

	grpcListener, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		telemetry.Logger.Fatal("Failed to listen for gRPC", zap.Error(err))
	}

	go func() {
		telemetry.Logger.Info("Payment Orchestrator gRPC starting", zap.String("grpc_port", cfg.GRPCPort))
		if err := grpcSrv.Serve(grpcListener); err != nil {
			telemetry.Logger.Fatal("Failed to start gRPC server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	telemetry.Logger.Info("Shutting down server...")

	// Stop outbox publisher first (uncomment when outbox is enabled)
	// outboxCancel()

	// Graceful shutdown gRPC
	grpcSrv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		telemetry.Logger.Error("Server forced to shutdown", zap.Error(err))
	}

	telemetry.Logger.Info("Server exited")
}
