package api

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/handlers"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
)

func NewRouter(repo interfaces.PaymentStateRepository, orchestrator *service.Orchestrator) http.Handler {
	mux := http.NewServeMux()

	// Prometheus metrics
	mux.Handle("GET /metrics", promhttp.Handler())

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "payment-orchestrator"})
	})

	// Payment routes
	paymentHandler := handlers.NewPaymentHandler(repo, orchestrator)
	mux.HandleFunc("GET /payments/{id}/state", paymentHandler.GetPaymentState)
	mux.HandleFunc("POST /payments/process", paymentHandler.ProcessPayment)

	// Apply global middleware
	var handler http.Handler = mux
	handler = telemetry.TracingMiddleware(handler)
	handler = telemetry.RecoveryMiddleware(handler)

	return handler
}
