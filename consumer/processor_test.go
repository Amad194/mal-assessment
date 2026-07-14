package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
)

// fakeStore is an in-memory AuditStore with a dedupe map and an optional
// transient-failure count, so we can exercise redelivery and retry paths.
type fakeStore struct {
	mu        sync.Mutex
	processed map[string]bool
	audits    int
	failTimes int // fail RecordAudit this many times before succeeding
	calls     int
}

func (f *fakeStore) RecordAudit(_ context.Context, e Event) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failTimes {
		return false, errors.New("transient db error")
	}
	if f.processed[e.EventID] {
		return false, nil // dedupe hit
	}
	f.processed[e.EventID] = true
	f.audits++
	return true, nil
}
func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) Close()                      {}

type fakeDLQ struct {
	mu    sync.Mutex
	count int
}

func (d *fakeDLQ) Publish(context.Context, []byte, []byte, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++
	return nil
}

func newProc(s AuditStore, d DeadLetter) *Processor {
	return NewProcessor(s, d, 3, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func msg(e Event) []byte { b, _ := json.Marshal(e); return b }

// The same event delivered three times must cause the effect exactly once.
func TestIdempotentRedelivery(t *testing.T) {
	s := &fakeStore{processed: map[string]bool{}}
	d := &fakeDLQ{}
	p := newProc(s, d)
	e := Event{EventID: "11111111-1111-1111-1111-111111111111", Type: "account.read", AccountID: "a", TS: "2026-01-01T00:00:00Z"}
	for i := 0; i < 3; i++ {
		if err := p.Handle(context.Background(), []byte("a"), msg(e)); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	if s.audits != 1 {
		t.Fatalf("want exactly 1 audit for 3 deliveries, got %d", s.audits)
	}
	if d.count != 0 {
		t.Fatalf("no DLQ expected, got %d", d.count)
	}
}

// Unparseable JSON is poison: park to DLQ, never retry, no side effect.
func TestPoisonMalformedJSONToDLQ(t *testing.T) {
	s := &fakeStore{processed: map[string]bool{}}
	d := &fakeDLQ{}
	if err := newProc(s, d).Handle(context.Background(), []byte("k"), []byte("{not json")); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if d.count != 1 || s.audits != 0 {
		t.Fatalf("want DLQ=1 audits=0, got DLQ=%d audits=%d", d.count, s.audits)
	}
}

// Missing required fields is also poison.
func TestPoisonInvalidFieldsToDLQ(t *testing.T) {
	s := &fakeStore{processed: map[string]bool{}}
	d := &fakeDLQ{}
	e := Event{EventID: "short", Type: "", AccountID: ""}
	if err := newProc(s, d).Handle(context.Background(), []byte("k"), msg(e)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if d.count != 1 {
		t.Fatalf("want DLQ=1, got %d", d.count)
	}
}

// A transient fault that clears within the retry budget must NOT dead-letter.
func TestTransientRetryThenSucceed(t *testing.T) {
	s := &fakeStore{processed: map[string]bool{}, failTimes: 2}
	d := &fakeDLQ{}
	e := Event{EventID: "22222222-2222-2222-2222-222222222222", Type: "account.read", AccountID: "a", TS: "2026-01-01T00:00:00Z"}
	if err := newProc(s, d).Handle(context.Background(), []byte("k"), msg(e)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if s.audits != 1 || d.count != 0 {
		t.Fatalf("want audits=1 DLQ=0, got audits=%d DLQ=%d", s.audits, d.count)
	}
}

// A transient fault that never clears is parked after the retry budget so it
// can't block the partition.
func TestTransientExhaustedToDLQ(t *testing.T) {
	s := &fakeStore{processed: map[string]bool{}, failTimes: 99}
	d := &fakeDLQ{}
	e := Event{EventID: "33333333-3333-3333-3333-333333333333", Type: "account.read", AccountID: "a", TS: "2026-01-01T00:00:00Z"}
	if err := newProc(s, d).Handle(context.Background(), []byte("k"), msg(e)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if d.count != 1 {
		t.Fatalf("want DLQ=1 after exhausting retries, got %d", d.count)
	}
}
