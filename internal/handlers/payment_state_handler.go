package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
)

type PaymentStateHandler struct {
	repo interfaces.PaymentStateRepository
}

func NewPaymentStateHandler(repo interfaces.PaymentStateRepository) *PaymentStateHandler {
	return &PaymentStateHandler{repo: repo}
}

func (h *PaymentStateHandler) GetPaymentState(c *gin.Context) {
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
