package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/models"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
)

type PaymentHandler struct {
	repo         interfaces.PaymentStateRepository
	orchestrator *service.Orchestrator
}

func NewPaymentHandler(repo interfaces.PaymentStateRepository, orchestrator *service.Orchestrator) *PaymentHandler {
	return &PaymentHandler{
		repo:         repo,
		orchestrator: orchestrator,
	}
}

func (h *PaymentHandler) GetPaymentState(c *gin.Context) {
	paymentID := c.Param("id")

	info, err := h.repo.GetByPaymentID(c.Request.Context(), paymentID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment state not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch payment state"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"payment_id":     paymentID,
		"state":          info.State,
		"previous_state": info.PreviousState,
		"fraud_decision": info.FraudDecision,
		"created_at":     info.CreatedAt,
		"updated_at":     info.UpdatedAt,
	})
}

func (h *PaymentHandler) ProcessPayment(c *gin.Context) {
	var event models.PaymentEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		telemetry.Logger.Error("Error decoding payment event", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.orchestrator.ProcessPayment(c.Request.Context(), &event); err != nil {
		telemetry.Logger.Error("Error processing payment",
			zap.String("payment_id", event.PaymentID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":      "Failed to process payment",
			"payment_id": event.PaymentID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "processed",
		"payment_id": event.PaymentID,
	})
}
