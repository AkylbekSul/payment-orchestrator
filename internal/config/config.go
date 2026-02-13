package config

import "os"

type Config struct {
	DatabaseURL    string
	RedisURL       string
	KafkaBrokers   string
	NatsURL        string
	JaegerEndpoint string
	Port           string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	return &Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisURL:       os.Getenv("REDIS_URL"),
		KafkaBrokers:   os.Getenv("KAFKA_BROKERS"),
		NatsURL:        os.Getenv("NATS_URL"),
		JaegerEndpoint: os.Getenv("JAEGER_ENDPOINT"),
		Port:           port,
	}
}
