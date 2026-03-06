package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *PaymentHandler) GetPaymentState(w http.ResponseWriter, r *http.Request) {
	paymentID := r.PathValue("id")

	info, err := h.repo.GetByPaymentID(r.Context(), paymentID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Payment state not found"})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch payment state"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"payment_id":     paymentID,
		"state":          info.State,
		"previous_state": info.PreviousState,
		"fraud_decision": info.FraudDecision,
		"created_at":     info.CreatedAt,
		"updated_at":     info.UpdatedAt,
	})
}

func (h *PaymentHandler) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	var event models.PaymentEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		telemetry.Logger.Error("Error decoding payment event", zap.Error(err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.orchestrator.ProcessPayment(r.Context(), &event); err != nil {
		telemetry.Logger.Error("Error processing payment",
			zap.String("payment_id", event.PaymentID),
			zap.Error(err),
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":      "Failed to process payment",
			"payment_id": event.PaymentID,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "processed",
		"payment_id": event.PaymentID,
	})
}
