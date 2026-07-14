package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the full runtime configuration, sourced from the environment.
// Secrets (DATABASE_URL) arrive via a projected Kubernetes Secret that is in
// turn hydrated from AWS Secrets Manager by External Secrets Operator; plain
// config arrives via a ConfigMap. See deploy/helm for the wiring.
type Config struct {
	Port            string
	DatabaseURL     string
	KafkaBrokers    []string
	KafkaTopic      string
	ShutdownDelay   time.Duration // pause after SIGTERM before closing listeners
	ShutdownTimeout time.Duration // hard cap on in-flight request draining
}

func LoadConfig() (Config, error) {
	c := Config{
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		KafkaTopic:      getenv("KAFKA_TOPIC", "accounts.audit"),
		ShutdownDelay:   getdur("SHUTDOWN_DELAY", 5*time.Second),
		ShutdownTimeout: getdur("SHUTDOWN_TIMEOUT", 25*time.Second),
	}
	if b := strings.TrimSpace(os.Getenv("KAFKA_BROKERS")); b != "" {
		c.KafkaBrokers = strings.Split(b, ",")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getdur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
