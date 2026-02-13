FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY . .

RUN go mod download && go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o payment-orchestrator ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/payment-orchestrator .

EXPOSE 8082

CMD ["./payment-orchestrator"]
