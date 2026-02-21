package config

import "os"

type Config struct {
	DatabaseURL     string
	RedisURL        string
	KafkaBrokers    string
	FraudServiceURL string
	JaegerEndpoint  string
	Port            string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	fraudServiceURL := os.Getenv("FRAUD_SERVICE_URL")
	if fraudServiceURL == "" {
		fraudServiceURL = "http://localhost:8083"
	}

	return &Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		KafkaBrokers:    os.Getenv("KAFKA_BROKERS"),
		FraudServiceURL: fraudServiceURL,
		JaegerEndpoint:  os.Getenv("JAEGER_ENDPOINT"),
		Port:            port,
	}
}
