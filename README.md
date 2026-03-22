# Payment Orchestrator Service

The Payment Orchestrator is the core service that manages the payment workflow state machine. It coordinates fraud checks via gRPC, publishes state change events to Kafka using the outbox pattern, and maintains payment state in PostgreSQL.

## Overview

The Payment Orchestrator is responsible for:
- Managing payment workflow state transitions
- Coordinating fraud checks via gRPC with the Fraud Service
- Publishing state change events to Kafka (outbox pattern)
- Maintaining payment state in PostgreSQL
- Distributed locking via Redis
- Providing both HTTP and gRPC APIs

## Architecture

```
Client / API Gateway
        ↓
Payment Orchestrator (HTTP :8082 / gRPC :50051)
        ↓
  State Machine:
  NEW → AUTH_PENDING → AUTHORIZED → CAPTURED → SUCCEEDED
                 ↓                                  ↓
              FAILED                             FAILED
        ↓                    ↓
  gRPC (fraud.check)    Kafka (payment.state.changed)
        ↓                    ↓
  Fraud Service         Ledger Service (downstream)
```

## State Machine

```
NEW
 ↓
AUTH_PENDING (fraud check request via gRPC)
 ↓
AUTHORIZED / FAILED
 ↓
CAPTURED (if authorized)
 ↓
SUCCEEDED / FAILED
```

### Valid State Transitions

| From State | To State | Trigger |
|------------|----------|---------|
| NEW | AUTH_PENDING | Automatic on creation |
| AUTH_PENDING | AUTHORIZED | Fraud check approved |
| AUTH_PENDING | FAILED | Fraud check denied or gRPC error |
| AUTHORIZED | CAPTURED | Automatic after authorization |
| CAPTURED | SUCCEEDED | Processing completed |
| CAPTURED | FAILED | Processing error |

### Payment States

- `NEW` — Initial state
- `AUTH_PENDING` — Waiting for fraud check response
- `AUTHORIZED` — Fraud check passed
- `CAPTURED` — Funds captured
- `SUCCEEDED` — Payment completed successfully
- `FAILED` — Payment failed at any stage
- `CANCELED` — Payment canceled

## Technology Stack

- **Language**: Go 1.22
- **Web Framework**: Gin
- **Database**: PostgreSQL (state persistence, outbox/inbox events)
- **Cache**: Redis (distributed locks)
- **Message Broker**: Kafka (event streaming via outbox pattern)
- **RPC**: gRPC (fraud service communication)
- **Observability**: OpenTelemetry + Jaeger + Prometheus
- **Logging**: Zap (Uber)

## Configuration

The service uses environment variables for configuration:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8082` |
| `GRPC_PORT` | gRPC server port | `50051` |
| `DATABASE_URL` | PostgreSQL connection string | Required |
| `REDIS_URL` | Redis server address | Required |
| `KAFKA_BROKERS` | Kafka broker addresses | Required |
| `FRAUD_SERVICE_GRPC_ADDR` | Fraud service gRPC address | `localhost:50052` |
| `JAEGER_ENDPOINT` | Jaeger collector endpoint | `jaeger:4318` |

## API Endpoints

### HTTP

#### Health Check
```http
GET /health
```
```json
{
  "status": "ok",
  "service": "payment-orchestrator"
}
```

#### Prometheus Metrics
```http
GET /metrics
```

#### Get Payment State
```http
GET /payments/:id/state
```
**Response** `200 OK`:
```json
{
  "payment_id": "payment-uuid",
  "state": "SUCCEEDED",
  "previous_state": "CAPTURED",
  "fraud_decision": "approve",
  "created_at": "2026-02-13T10:00:00Z",
  "updated_at": "2026-02-13T10:00:05Z"
}
```

#### Process Payment
```http
POST /payments/process
```
**Request Body**:
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
**Response** `200 OK`:
```json
{
  "status": "processed",
  "payment_id": "payment-uuid"
}
```

### gRPC

- `PaymentOrchestrator.ProcessPayment` — Initiates payment processing
- `PaymentOrchestrator.GetPaymentState` — Retrieves payment state

Proto definitions are in the shared `proto/` module.

## Database Schema

### payment_states
```sql
CREATE TABLE payment_states (
    payment_id VARCHAR(255) PRIMARY KEY,
    state VARCHAR(50) NOT NULL,
    previous_state VARCHAR(50),
    fraud_decision VARCHAR(50),
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### outbox_events (Outbox Pattern)
```sql
CREATE TABLE outbox_events (
    id BIGSERIAL PRIMARY KEY,
    aggregate_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    published BOOLEAN DEFAULT FALSE,
    published_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### inbox_events (Inbox Pattern)
```sql
CREATE TABLE inbox_events (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(255) UNIQUE NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    processed BOOLEAN DEFAULT FALSE,
    processed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Reliability Patterns

### Outbox Pattern
State transitions and outbox events are written in a single database transaction. A background publisher polls the outbox table every second and publishes unpublished events to Kafka in batches, ensuring reliable event delivery.

### Inbox Pattern
The inbox table provides idempotent event processing, preventing duplicate events from being handled.

### Distributed Locking
Redis-based locks (`payment_lock:{paymentID}`) prevent concurrent updates to the same payment. Lock timeout: 30 seconds.

## Kafka Events

### Producer: `payment.state.changed`

Published via the outbox pattern for downstream consumers (e.g., Ledger Service).

## Project Structure
```
payment-orchestrator/
├── cmd/
│   └── main.go                          # Application entry point
├── internal/
│   ├── api/
│   │   └── router.go                    # HTTP routing setup
│   ├── config/
│   │   └── config.go                    # Configuration management
│   ├── grpcserver/
│   │   └── payment_grpc_server.go       # gRPC service implementation
│   ├── handlers/
│   │   └── payment_handler.go           # HTTP request handlers
│   ├── interfaces/
│   │   └── payment_state_repository.go  # Repository interface
│   ├── models/
│   │   └── payment.go                   # Domain models and constants
│   ├── repository/
│   │   └── payment_state_repository.go  # Data access layer
│   ├── service/
│   │   └── orchestrator.go              # Core orchestration logic
│   └── telemetry/
│       └── telemetry.go                 # Observability setup
├── migrations/
│   └── 001_payment_orchestrator_schema.sql
├── Dockerfile
├── go.mod
└── README.md
```

## Running the Service

### Local Development

1. Set up environment variables:
```bash
export DATABASE_URL="postgresql://user:password@localhost:5432/payment_orchestrator?sslmode=disable"
export REDIS_URL="localhost:6379"
export KAFKA_BROKERS="localhost:9092"
export FRAUD_SERVICE_GRPC_ADDR="localhost:50052"
export PORT="8082"
export GRPC_PORT="50051"
```

2. Start the service:
```bash
go run cmd/main.go
```

### Docker

```bash
docker build -t payment-orchestrator .
docker run -p 8082:8082 -p 50051:50051 \
  -e DATABASE_URL="..." \
  -e REDIS_URL="..." \
  -e KAFKA_BROKERS="..." \
  -e FRAUD_SERVICE_GRPC_ADDR="..." \
  payment-orchestrator
```

### Docker Compose

```bash
docker-compose up payment-orchestrator
```

## Dependencies

### External Services
- **PostgreSQL** — Payment state persistence, outbox/inbox events
- **Redis** — Distributed locks
- **Kafka** — Event streaming (producer only)
- **Fraud Service** — Fraud checks via gRPC
- **Jaeger** (optional) — Distributed tracing

### Integration Points
- **Fraud Service** (upstream) — gRPC fraud check requests
- **Ledger Service** (downstream) — Consumes `payment.state.changed` events from Kafka

## Graceful Shutdown

The service supports graceful shutdown:
1. Stops the outbox publisher
2. Gracefully stops the gRPC server
3. Shuts down the HTTP server (5-second timeout)
4. Closes Kafka writer, Redis, and database connections
5. Flushes telemetry

## Running Tests
```bash
go test ./...
```

## License

Copyright 2026
