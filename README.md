# Payment Orchestrator Service

The Payment Orchestrator is the core service that manages the payment workflow state machine. It coordinates between multiple services, handles fraud checks, manages payment states, and ensures reliable payment processing through a distributed architecture.

## Overview

The Payment Orchestrator is responsible for:
- Managing payment workflow state transitions
- Coordinating fraud checks via NATS
- Publishing state change events to Kafka
- Maintaining payment state in PostgreSQL
- Implementing saga pattern for distributed transactions
- Handling retries and error scenarios
- Providing payment state query APIs

## Architecture

```
API Gateway → Kafka (payment.created)
                ↓
        Payment Orchestrator
                ↓
         State Machine:
         NEW → FRAUD_CHECK → APPROVED/REJECTED → PROCESSING → COMPLETED/FAILED
                ↓                    ↓
         NATS (fraud.check)    Kafka (payment.state.changed)
                ↓
         Fraud Service
```

## State Machine

```
NEW
 ↓
FRAUD_CHECK (request to Fraud Service)
 ↓
APPROVED / REJECTED
 ↓
PROCESSING (if approved)
 ↓
COMPLETED / FAILED
 ↓
REFUNDED (if needed)
```

## Technology Stack

- **Language**: Go 1.21+
- **Web Framework**: Gin
- **Database**: PostgreSQL (state persistence)
- **Cache**: Redis (distributed locks, state cache)
- **Message Brokers**: 
  - Kafka (event streaming)
  - NATS (request/response with fraud service)
- **Observability**: OpenTelemetry + Jaeger + Prometheus
- **Logging**: Zap (Uber)

## Configuration

The service uses environment variables for configuration:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8082` |
| `DATABASE_URL` | PostgreSQL connection string | Required |
| `REDIS_URL` | Redis server address | Required |
| `KAFKA_BROKERS` | Kafka broker addresses | Required |
| `NATS_URL` | NATS server address | Required |
| `JAEGER_ENDPOINT` | Jaeger collector endpoint | Optional |

## API Endpoints

### Get Payment State
```http
GET /payments/:id/state
```

**Response**: `200 OK`
```json
{
  "payment_id": "payment-uuid",
  "current_state": "APPROVED",
  "previous_state": "FRAUD_CHECK",
  "updated_at": "2026-02-13T10:00:05Z",
  "metadata": {
    "fraud_score": 0.15,
    "fraud_reason": "Low risk transaction"
  },
  "history": [
    {
      "state": "NEW",
      "timestamp": "2026-02-13T10:00:00Z"
    },
    {
      "state": "FRAUD_CHECK",
      "timestamp": "2026-02-13T10:00:02Z"
    },
    {
      "state": "APPROVED",
      "timestamp": "2026-02-13T10:00:05Z"
    }
  ]
}
```

### Health Check
```http
GET /health
```

**Response**: `200 OK`
```json
{
  "status": "ok",
  "service": "payment-orchestrator"
}
```

### Prometheus Metrics
```http
GET /metrics
```

## Event-Driven Workflow

### Kafka Consumer: `payment.created`

Listens for new payment events from API Gateway.

**Input Event**:
```json
{
  "payment_id": "payment-uuid",
  "amount": 100.50,
  "currency": "USD",
  "customer_id": "customer-123",
  "merchant_id": "merchant-456",
  "status": "NEW",
  "created_at": "2026-02-13T10:00:00Z"
}
```

**Processing Flow**:
1. Receive payment.created event
2. Store initial state in database
3. Transition to FRAUD_CHECK state
4. Request fraud check via NATS
5. Wait for response or timeout

### NATS Request/Response: `fraud.check`

Synchronous fraud check communication.

**Request to Fraud Service**:
```json
{
  "payment_id": "payment-uuid",
  "customer_id": "customer-123",
  "amount": 100.50,
  "currency": "USD",
  "merchant_id": "merchant-456"
}
```

**Response from Fraud Service**:
```json
{
  "payment_id": "payment-uuid",
  "approved": true,
  "risk_score": 0.15,
  "reason": "Low risk transaction"
}
```

**Timeout**: 5 seconds (configurable)

### Kafka Producer: `payment.state.changed`

Publishes state change events for downstream services.

**Output Event**:
```json
{
  "payment_id": "payment-uuid",
  "previous_state": "NEW",
  "current_state": "APPROVED",
  "amount": 100.50,
  "currency": "USD",
  "customer_id": "customer-123",
  "merchant_id": "merchant-456",
  "changed_at": "2026-02-13T10:00:05Z",
  "metadata": {
    "fraud_score": 0.15,
    "fraud_reason": "Low risk transaction"
  }
}
```

## State Transitions

### Valid State Transitions

| From State | To State | Trigger |
|------------|----------|---------|
| NEW | FRAUD_CHECK | Automatic on creation |
| FRAUD_CHECK | APPROVED | Fraud check passed |
| FRAUD_CHECK | REJECTED | Fraud check failed |
| APPROVED | PROCESSING | Manual or automatic |
| PROCESSING | COMPLETED | Processing succeeded |
| PROCESSING | FAILED | Processing failed |
| COMPLETED | REFUNDED | Refund requested |
| FAILED | RETRY | Retry requested |

### State Metadata

Each state can store metadata:
- **FRAUD_CHECK**: fraud request timestamp
- **APPROVED**: risk_score, fraud_reason
- **REJECTED**: risk_score, rejection_reason
- **FAILED**: error_code, error_message, retry_count
- **COMPLETED**: completion_timestamp, confirmation_id

## Database Schema

### Payment States Table
```sql
CREATE TABLE payment_states (
    id BIGSERIAL PRIMARY KEY,
    payment_id VARCHAR(255) NOT NULL UNIQUE,
    current_state VARCHAR(50) NOT NULL,
    previous_state VARCHAR(50),
    amount DECIMAL(19,4) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    customer_id VARCHAR(255) NOT NULL,
    merchant_id VARCHAR(255) NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_payment_id (payment_id),
    INDEX idx_current_state (current_state),
    INDEX idx_customer_id (customer_id),
    INDEX idx_updated_at (updated_at DESC)
);

CREATE TABLE payment_state_history (
    id BIGSERIAL PRIMARY KEY,
    payment_id VARCHAR(255) NOT NULL,
    state VARCHAR(50) NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_payment_id_history (payment_id, created_at DESC)
);
```

## Key Features

### Saga Pattern Implementation

The orchestrator implements a saga pattern for distributed transactions:

```
BEGIN SAGA
  1. Save payment state
  2. Request fraud check (compensatable)
  3. If approved: continue processing
  4. If rejected: mark as rejected (compensation)
  5. Process payment
  6. If failed: retry or mark as failed (compensation)
END SAGA
```

### Distributed Locking

Uses Redis for distributed locking to prevent concurrent state updates:

```go
// Acquire lock before state transition
lock := redis.Lock(fmt.Sprintf("payment:%s", paymentID), 10*time.Second)
defer lock.Release()

// Perform state transition
```

### Idempotency

- State transitions are idempotent
- Duplicate events are safely ignored
- Kafka offsets committed only after successful processing

### Retry Logic

Automatic retries for transient failures:
```go
RetryConfig{
    MaxRetries:      3,
    InitialDelay:    1 * time.Second,
    MaxDelay:        30 * time.Second,
    BackoffFactor:   2.0,
    RetryableErrors: []string{"timeout", "connection_error"},
}
```

### Circuit Breaker

Protects against cascading failures:
```go
CircuitBreakerConfig{
    Threshold:   5,              // failures before opening
    Timeout:     60 * time.Second, // time before retry
    MaxRequests: 1,              // test requests in half-open
}
```

## Running the Service

### Local Development

1. Set up environment variables:
```bash
export DATABASE_URL="postgresql://user:password@localhost:5432/payment_orchestrator?sslmode=disable"
export REDIS_URL="localhost:6379"
export KAFKA_BROKERS="localhost:9092"
export NATS_URL="nats://localhost:4222"
export PORT="8082"
```

2. Run database migrations:
```bash
# Apply migrations from migrations/ directory
```

3. Start the service:
```bash
go run cmd/main.go
```

### Docker

Build and run with Docker:
```bash
docker build -t payment-orchestrator .
docker run -p 8082:8082 \
  -e DATABASE_URL="..." \
  -e REDIS_URL="..." \
  -e KAFKA_BROKERS="..." \
  -e NATS_URL="..." \
  payment-orchestrator
```

### Docker Compose

Run as part of the complete system:
```bash
docker-compose up payment-orchestrator
```

## Dependencies

### External Services
- **PostgreSQL**: Payment state persistence
- **Redis**: Distributed locks and caching
- **Kafka**: Event streaming (consumer and producer)
- **NATS**: Request/response with fraud service
- **Jaeger** (optional): Distributed tracing

### Integration Points
- **API Gateway** (upstream): Produces `payment.created` events
- **Fraud Service**: Fraud check requests via NATS
- **Ledger Service** (downstream): Consumes `payment.state.changed` events

## Monitoring

### Prometheus Metrics

Custom metrics exposed:
```
# State machine metrics
payment_state_transitions_total{from_state,to_state}
payment_state_transition_duration_seconds{state}
payment_current_state_gauge{state}

# Workflow metrics
payment_processing_duration_seconds
payment_fraud_check_duration_seconds
payment_success_rate

# Error metrics
payment_state_transition_errors_total{state,error_type}
payment_fraud_check_timeouts_total
payment_retries_total{state}

# Kafka metrics
kafka_consumer_lag{topic}
kafka_messages_processed_total{topic}
kafka_producer_errors_total
```

### Key Alerts

Recommended alerts:
```yaml
# High state transition failure rate
- alert: HighStateTransitionFailures
  expr: rate(payment_state_transition_errors_total[5m]) > 0.05
  
# Fraud check timeouts
- alert: FraudCheckTimeouts
  expr: rate(payment_fraud_check_timeouts_total[5m]) > 0.1

# Kafka consumer lag
- alert: OrchestratorKafkaLag
  expr: kafka_consumer_lag{service="orchestrator"} > 1000

# Low payment success rate
- alert: LowPaymentSuccessRate
  expr: rate(payment_success_rate[15m]) < 0.9
```

## Development

### Project Structure
```
payment-orchestrator/
├── cmd/
│   └── main.go                        # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go                  # Configuration management
│   ├── handlers/
│   │   └── payment_state_handler.go   # HTTP handlers
│   ├── interfaces/
│   │   └── payment_state_repository.go # Repository interface
│   ├── models/
│   │   └── payment.go                 # Domain models
│   ├── repository/
│   │   └── payment_state_repository.go # Data access layer
│   ├── service/
│   │   └── orchestrator.go            # Orchestration logic
│   └── telemetry/
│       └── telemetry.go               # Observability setup
├── migrations/
│   └── 001_payment_orchestrator_schema.sql
├── Dockerfile
└── README.md
```

### Running Tests
```bash
go test ./...
```

### Adding New States

To add a new state to the state machine:

1. Define the new state constant
2. Add valid transitions in state machine
3. Implement state handler
4. Update state change event publisher
5. Add metrics and logging
6. Update tests and documentation

## Error Handling

### Transient Errors
- Automatic retry with exponential backoff
- Circuit breaker for repeated failures
- Fallback to manual review queue

### Permanent Errors
- State marked as FAILED
- Error details stored in metadata
- Alert sent to operations team
- Customer notification triggered

### Timeout Scenarios
- Fraud check timeout (5s): Mark for manual review
- Database timeout: Retry with backoff
- Kafka publish failure: Local queue for retry

## Compensation Logic

When a saga step fails, compensation is triggered:

```go
func (o *Orchestrator) CompensatePayment(paymentID string) error {
    // Rollback logic
    if err := o.transitionState(paymentID, "FAILED"); err != nil {
        return err
    }
    
    // Publish compensation event
    o.publishStateChange(paymentID, "COMPENSATED")
    
    // Notify customer
    o.notifyCustomer(paymentID, "PAYMENT_FAILED")
    
    return nil
}
```

## Performance Considerations

### Kafka Consumer Tuning
```go
kafka.ReaderConfig{
    Brokers:         cfg.KafkaBrokers,
    Topic:           "payment.created",
    GroupID:         "payment-orchestrator-group",
    MinBytes:        1024,
    MaxBytes:        10485760,
    CommitInterval:  time.Second,
    StartOffset:     kafka.FirstOffset,
}
```

### Database Optimization
- Connection pooling (10-100 connections)
- Prepared statements
- Batch updates where possible
- Index on state and payment_id

### Redis Performance
- Connection pooling
- Pipeline commands where possible
- Lock timeout tuning
- Key expiration strategy

## Security Considerations

- No sensitive payment data logged
- Encrypted database connections
- Redis authentication enabled
- Rate limiting on APIs
- Input validation on all events
- RBAC for database access

## Disaster Recovery

### Failure Scenarios

1. **Orchestrator crashes**: Kafka consumer group rebalances, another instance picks up
2. **Database failure**: Service becomes read-only, alerts triggered
3. **Fraud service down**: Payments queued or marked for manual review
4. **Kafka unavailable**: Events buffered in memory, then disk

### Recovery Steps
1. Restore database from backup
2. Reset Kafka consumer offset if needed
3. Replay missed events
4. Verify state consistency
5. Resume normal operation

## Graceful Shutdown

The service supports graceful shutdown:
- Stops consuming from Kafka
- Completes in-flight state transitions
- Commits Kafka offsets
- Releases Redis locks
- Closes all connections
- Flushes metrics and traces
- 5-second timeout

## Future Enhancements

- **Scheduled Payments**: Support for future-dated payments
- **Recurring Payments**: Subscription payment handling
- **Multi-currency**: Currency conversion support
- **Payment Split**: Split payments across multiple merchants
- **Advanced Retry**: ML-based retry strategies
- **Webhooks**: Customer webhook notifications

## License

Copyright © 2026
