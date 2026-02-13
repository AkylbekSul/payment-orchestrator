package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Tracer      trace.Tracer
	Logger      *zap.Logger
	ServiceName string
)

// InitTelemetry initializes OpenTelemetry tracing and structured logging
func InitTelemetry(serviceName string) error {
	ServiceName = serviceName

	// Initialize structured logger
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	
	var err error
	Logger, err = config.Build()
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Initialize OpenTelemetry
	jaegerEndpoint := os.Getenv("JAEGER_ENDPOINT")
	if jaegerEndpoint == "" {
		jaegerEndpoint = "jaeger:4318" // OTLP HTTP endpoint
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(jaegerEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	Tracer = otel.Tracer(serviceName)

	Logger.Info("Telemetry initialized", zap.String("service", serviceName))
	return nil
}

// Shutdown gracefully shuts down telemetry
func Shutdown(ctx context.Context) error {
	if tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		if err := tp.Shutdown(ctx); err != nil {
			return err
		}
	}
	return Logger.Sync()
}

// TracingMiddleware adds tracing and logging to Gin routes
func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract trace context from headers
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		
		// Start new span
		ctx, span := Tracer.Start(ctx, fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()))
		defer span.End()

		// Add trace context to request
		c.Request = c.Request.WithContext(ctx)

		// Add trace ID to response headers
		spanCtx := span.SpanContext()
		if spanCtx.IsValid() {
			c.Header("X-Trace-ID", spanCtx.TraceID().String())
		}

		// Record request start time
		start := time.Now()
		
		// Process request
		c.Next()
		
		// Calculate duration
		duration := time.Since(start)

		// Add span attributes
		span.SetAttributes(
			semconv.HTTPMethodKey.String(c.Request.Method),
			semconv.HTTPRouteKey.String(c.FullPath()),
			semconv.HTTPStatusCodeKey.Int(c.Writer.Status()),
			attribute.String("http.client_ip", c.ClientIP()),
		)

		// Log request
		Logger.Info("HTTP request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", duration),
			zap.String("trace_id", spanCtx.TraceID().String()),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}
