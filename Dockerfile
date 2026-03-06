FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy proto module (needed by replace directive in go.mod)
COPY proto/ /app/proto/

# Copy service source
COPY services/payment-orchestrator/ /app/services/payment-orchestrator/

WORKDIR /app/services/payment-orchestrator

RUN go mod download && go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o payment-orchestrator ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/services/payment-orchestrator/payment-orchestrator .

EXPOSE 8082 50051

CMD ["./payment-orchestrator"]
