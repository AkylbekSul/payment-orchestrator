package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/akylbek/payment-system/payment-orchestrator/internal/handlers"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/interfaces"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/service"
	"github.com/akylbek/payment-system/payment-orchestrator/internal/telemetry"
)

func NewRouter(repo interfaces.PaymentStateRepository, orchestrator *service.Orchestrator) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(telemetry.TracingMiddleware())

	// Prometheus metrics
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "payment-orchestrator"})
	})

	// Payment routes
	paymentHandler := handlers.NewPaymentHandler(repo, orchestrator)
	r.GET("/payments/:id/state", paymentHandler.GetPaymentState)
	r.POST("/payments/process", paymentHandler.ProcessPayment)

	return r
}
