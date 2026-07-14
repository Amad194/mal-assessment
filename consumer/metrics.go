package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RED-style metrics for the consumer.
var (
	messagesProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "consumer_messages_processed_total",
		Help: "Audit events successfully recorded (side effect applied).",
	})
	messagesDeduped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "consumer_messages_deduplicated_total",
		Help: "Redelivered events skipped by the idempotency guard.",
	})
	messagesDeadLettered = promauto.NewCounter(prometheus.CounterOpts{
		Name: "consumer_messages_dead_lettered_total",
		Help: "Poison / persistently-failing events routed to the DLQ.",
	})
	processingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "consumer_processing_duration_seconds",
		Help:    "Per-message processing latency.",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	})
)
