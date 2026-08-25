// Package kafka publishes accepted events to the durable log.
//
// The Producer interface exists so the HTTP layer can be tested without a
// broker, and so the "an invalid event never reaches Kafka" claim can be
// asserted rather than assumed: the test uses a recording producer and checks
// it stayed empty.
package kafka

import (
	"context"
	"sync"
)

type Message struct {
	Topic string
	Key   []byte // tenant id: keeps one tenant's events in order on one partition
	Value []byte
}

type Producer interface {
	Publish(ctx context.Context, m Message) error
	Close() error
}

// Recorder is the test double. It is in the production package on purpose:
// the interface and its reference implementation belong together, and this one
// is twenty lines.
type Recorder struct {
	mu       sync.Mutex
	Messages []Message
	Err      error
}

func (r *Recorder) Publish(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	r.Messages = append(r.Messages, m)
	return nil
}

func (r *Recorder) Close() error { return nil }

func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Messages)
}
