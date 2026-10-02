package runs

import (
	"sync"
)

// subscriber receives live events of one run. The channel is buffered; a
// subscriber that falls too far behind is closed with lagged set, and the
// reader catches up from the database before subscribing again.
type subscriber struct {
	ch     chan Event
	lagged bool
	closed bool
}

// broker fans one run's events out to its live subscribers.
type broker struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool
}

const subscriberBuffer = 1024

func newBroker() *broker {
	return &broker{subs: map[*subscriber]struct{}{}}
}

// subscribe registers a subscriber. It returns nil when the broker is already
// closed, which means the run has finished and the database holds everything.
func (b *broker) subscribe() *subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	s := &subscriber{ch: make(chan Event, subscriberBuffer)}
	b.subs[s] = struct{}{}
	return s
}

func (b *broker) unsubscribe(s *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		if !s.closed {
			s.closed = true
			close(s.ch)
		}
	}
}

// publish delivers an event to every subscriber without blocking the run.
func (b *broker) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			s.lagged = true
			s.closed = true
			close(s.ch)
			delete(b.subs, s)
		}
	}
}

// close ends every subscription; the run is over.
func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for s := range b.subs {
		if !s.closed {
			s.closed = true
			close(s.ch)
		}
		delete(b.subs, s)
	}
}
