package telemetry

import (
	"sync"
)

// Broadcaster fans SSE payloads out to every connected dashboard client.
// Send is non-blocking: a slow client is dropped rather than stalling the
// telemetry loop.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[chan string]struct{})}
}

// Subscribe returns a channel that receives payloads until Unsubscribe.
func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

// Publish sends payload to all subscribers; full channels are skipped.
func (b *Broadcaster) Publish(payload string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- payload:
		default:
		}
	}
}
