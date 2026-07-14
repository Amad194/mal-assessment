package main

import "time"

// Event is the audit event emitted by accounts-api onto the accounts.audit topic.
type Event struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	AccountID string `json:"account_id"`
	TS        string `json:"ts"`
}

// OccurredAt parses the RFC3339 timestamp, falling back to now on a bad value so
// a slightly malformed timestamp doesn't poison an otherwise valid audit event.
func (e Event) OccurredAt() time.Time {
	if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
		return t
	}
	return time.Now().UTC()
}

// valid reports whether the event has the fields required to be processed at all.
// Anything failing this is a poison message (park to DLQ, never retry).
func (e Event) valid() bool {
	return len(e.EventID) == 36 && e.AccountID != "" && e.Type != ""
}
