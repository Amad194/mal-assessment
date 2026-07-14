package main

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
)

// KafkaDLQ writes poison / exhausted messages to the dead-letter topic, tagging
// them with the reason and time so an operator can triage and replay.
type KafkaDLQ struct{ w *kafka.Writer }

func NewKafkaDLQ(brokers []string, topic string) *KafkaDLQ {
	return &KafkaDLQ{w: &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		WriteTimeout: 5 * time.Second,
	}}
}

func (d *KafkaDLQ) Publish(ctx context.Context, key, value []byte, reason string) error {
	return d.w.WriteMessages(ctx, kafka.Message{
		Key:   key,
		Value: value,
		Headers: []kafka.Header{
			{Key: "x-dlq-reason", Value: []byte(reason)},
			{Key: "x-dlq-at", Value: []byte(time.Now().UTC().Format(time.RFC3339))},
		},
	})
}

func (d *KafkaDLQ) Close() error { return d.w.Close() }
