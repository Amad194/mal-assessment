package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
)

// Publisher is the messaging port. Reads emit a best-effort audit event onto
// Kafka (mapped to AWS MSK) so downstream fraud/audit consumers get a durable
// trail. Publishing is intentionally off the request's critical path: a Kafka
// outage degrades auditing but must not fail an account read.
type Publisher interface {
	Publish(ctx context.Context, key string, event any) error
	Close() error
}

type KafkaPublisher struct {
	w *kafka.Writer
}

func NewKafkaPublisher(brokers []string, topic string) *KafkaPublisher {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{}, // partition by key (account id) for ordering
		RequiredAcks: kafka.RequireAll,
		WriteTimeout: 3 * time.Second,
		Async:        false,
	}
	return &KafkaPublisher{w: w}
}

func (k *KafkaPublisher) Publish(ctx context.Context, key string, event any) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return k.w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: b,
	})
}

func (k *KafkaPublisher) Close() error { return k.w.Close() }

// NoopPublisher is used when KAFKA_BROKERS is unset (e.g. local dev, tests).
type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, any) error { return nil }
func (NoopPublisher) Close() error                               { return nil }
