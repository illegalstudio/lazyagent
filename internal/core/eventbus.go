package core

import (
	"sync"
	"time"
)

// SessionEvent is published when a session's resolved activity changes.
type SessionEvent struct {
	SessionID   string
	Agent       string
	From        ActivityKind
	To          ActivityKind
	At          time.Time
	ProjectPath string
}

// EventBus is a minimal typed pub-sub for in-process subscribers.
// Publish never blocks; events are dropped for subscribers whose channel is full.
type EventBus struct {
	mu   sync.RWMutex
	subs []chan SessionEvent
}

// NewEventBus returns a ready-to-use EventBus.
func NewEventBus() *EventBus { return &EventBus{} }

// Subscribe registers a new subscriber and returns its channel.
// buf is the channel buffer; pick a size matching the subscriber's drain rate.
func (b *EventBus) Subscribe(buf int) <-chan SessionEvent {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan SessionEvent, buf)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel from the bus. Safe to call multiple times.
// The caller must not read from the channel after Unsubscribe returns.
func (b *EventBus) Unsubscribe(ch <-chan SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, sub := range b.subs {
		if sub == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

// Publish sends e to every subscriber. Non-blocking: subscribers whose channel
// is full miss this event.
func (b *EventBus) Publish(e SessionEvent) {
	b.mu.RLock()
	subs := append([]chan SessionEvent(nil), b.subs...)
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}
