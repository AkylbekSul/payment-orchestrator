package config

import "os"

type Config struct {
	DatabaseURL      string
	RedisURL         string
	KafkaBrokers     string
	FraudServiceAddr string
	JaegerEndpoint   string
	Port             string
	GRPCPort         string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "50051"
	}

	fraudServiceAddr := os.Getenv("FRAUD_SERVICE_GRPC_ADDR")
	if fraudServiceAddr == "" {
		fraudServiceAddr = "localhost:50052"
	}

	return &Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RedisURL:         os.Getenv("REDIS_URL"),
		KafkaBrokers:     os.Getenv("KAFKA_BROKERS"),
		FraudServiceAddr: fraudServiceAddr,
		JaegerEndpoint:   os.Getenv("JAEGER_ENDPOINT"),
		Port:             port,
		GRPCPort:         grpcPort,
	}
}
