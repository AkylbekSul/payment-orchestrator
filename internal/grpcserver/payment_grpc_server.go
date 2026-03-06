package grpcserver

import (
	"context"
	"database/sql"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
	paymentpb "github.com/akylbek/payment-system/proto/payment"
)

type PaymentGRPCServer struct {
	paymentpb.UnimplementedPaymentOrchestratorServer
	repo         interfaces.PaymentStateRepository
	orchestrator *service.Orchestrator
}

func NewPaymentGRPCServer(repo interfaces.PaymentStateRepository, orchestrator *service.Orchestrator) *PaymentGRPCServer {
	return &PaymentGRPCServer{
		repo:         repo,
		orchestrator: orchestrator,
	}
}

func (s *PaymentGRPCServer) ProcessPayment(ctx context.Context, req *paymentpb.ProcessPaymentRequest) (*paymentpb.ProcessPaymentResponse, error) {
	telemetry.Logger.Info("gRPC ProcessPayment request",
		zap.String("payment_id", req.PaymentId),
		zap.Float64("amount", req.Amount),
	)

	createdAt, _ := time.Parse(time.RFC3339, req.CreatedAt)

	event := &models.PaymentEvent{
		PaymentID:  req.PaymentId,
		Amount:     req.Amount,
		Currency:   req.Currency,
		CustomerID: req.CustomerId,
		MerchantID: req.MerchantId,
		Status:     req.Status,
		CreatedAt:  createdAt,
	}

	if err := s.orchestrator.ProcessPayment(ctx, event); err != nil {
		telemetry.Logger.Error("Error processing payment via gRPC",
			zap.String("payment_id", req.PaymentId),
			zap.Error(err),
		)
		return nil, status.Errorf(codes.Internal, "failed to process payment: %v", err)
	}

	return &paymentpb.ProcessPaymentResponse{
		Status:    "processed",
		PaymentId: req.PaymentId,
	}, nil
}

func (s *PaymentGRPCServer) GetPaymentState(ctx context.Context, req *paymentpb.GetPaymentStateRequest) (*paymentpb.GetPaymentStateResponse, error) {
	info, err := s.repo.GetByPaymentID(ctx, req.PaymentId)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "payment state not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to fetch payment state: %v", err)
	}

	return &paymentpb.GetPaymentStateResponse{
		PaymentId:     req.PaymentId,
		State:         info.State,
		PreviousState: info.PreviousState,
		FraudDecision: info.FraudDecision,
		CreatedAt:     info.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     info.UpdatedAt.Format(time.RFC3339),
	}, nil
}
