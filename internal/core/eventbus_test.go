package core

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(4)
	defer bus.Unsubscribe(ch)

	want := SessionEvent{SessionID: "s1", From: ActivityIdle, To: ActivityThinking, At: time.Unix(0, 0)}
	bus.Publish(want)

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event")
	}
}

func TestEventBus_DropOnFullSubscriber(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1)
	defer bus.Unsubscribe(ch)

	bus.Publish(SessionEvent{SessionID: "a"})
	bus.Publish(SessionEvent{SessionID: "b"}) // dropped
	bus.Publish(SessionEvent{SessionID: "c"}) // dropped

	got := <-ch
	if got.SessionID != "a" {
		t.Fatalf("got %q, want %q", got.SessionID, "a")
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra event: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBus_UnsubscribeIdempotent(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1)
	bus.Unsubscribe(ch)
	bus.Unsubscribe(ch) // must not panic

	// Publish after unsubscribe must not block or send to the closed channel.
	done := make(chan struct{})
	go func() {
		bus.Publish(SessionEvent{SessionID: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish after Unsubscribe blocked")
	}
}

func TestEventBus_PublishConcurrentUnsubscribe(t *testing.T) {
	// Exercise the race between Publish iterating the subscriber list and
	// Unsubscribe mutating it. Run under `-race`.
	bus := NewEventBus()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		ch := bus.Subscribe(16)
		wg.Add(2)
		go func(ch <-chan SessionEvent) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				bus.Publish(SessionEvent{SessionID: "x"})
			}
		}(ch)
		go func(ch <-chan SessionEvent) {
			defer wg.Done()
			time.Sleep(time.Microsecond)
			bus.Unsubscribe(ch)
		}(ch)
	}
	wg.Wait()
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1024)
	defer bus.Unsubscribe(ch)

	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bus.Publish(SessionEvent{SessionID: "x"})
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(50 * time.Millisecond):
			if count != n {
				t.Fatalf("received %d events, want %d", count, n)
			}
			return
		}
	}
}
