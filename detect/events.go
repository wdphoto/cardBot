package detect

import "sync"

// Event is one observed transition, in observation order. Exactly one of
// Card (arrival) and RemovedPath (removal) is set.
type Event struct {
	Card        *Card
	RemovedPath string
}

// One stream per detector run. Producers may wait for capacity, but Stop
// interrupts delivery. No tracking lock is held while waiting: a consumer
// can still forget a programmatically ejected card.
type cardStream struct {
	events chan Event
	stop   chan struct{}
	order  sync.Mutex // serialize native callbacks and background observations
	mu     sync.Mutex
	cards  map[string]bool
}

func newCardStream() *cardStream {
	return &cardStream{events: make(chan Event, 10), stop: make(chan struct{}), cards: make(map[string]bool)}
}
func (s *cardStream) tracked(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cards[path]
}
func (s *cardStream) forget(path string) {
	s.mu.Lock()
	delete(s.cards, path)
	s.mu.Unlock()
}
func (s *cardStream) observe(e Event) bool {
	s.order.Lock()
	defer s.order.Unlock()
	s.mu.Lock()
	if e.Card != nil {
		if s.cards[e.Card.Path] {
			s.mu.Unlock()
			return false
		}
		s.cards[e.Card.Path] = true
	} else {
		if !s.cards[e.RemovedPath] {
			s.mu.Unlock()
			return false
		}
		delete(s.cards, e.RemovedPath)
	}
	s.mu.Unlock()
	select {
	case s.events <- e:
		return true
	case <-s.stop:
		return false
	}
}
func (s *cardStream) removeMissing(present map[string]bool) {
	s.mu.Lock()
	var missing []string
	for path := range s.cards {
		if !present[path] {
			missing = append(missing, path)
		}
	}
	s.mu.Unlock()
	for _, path := range missing {
		s.observe(Event{RemovedPath: path})
	}
}
