package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Client is the real producer.
//
// Publish waits for the broker to acknowledge before returning. That is a
// deliberate cost: the alternative is accepting an event, returning 202, and
// losing it if the process dies before the batch flushes. An ingest service
// that says "accepted" must mean it.
type Client struct {
	kc *kgo.Client
}

func Dial(brokers []string) (*Client, error) {
	kc, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ProducerBatchCompression(kgo.SnappyCompression()),
		// Idempotent producer: a retried batch cannot land twice on the log.
		// The event id makes the pipeline idempotent end to end; this makes
		// the broker leg idempotent too.
		kgo.ProducerLinger(0),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka: %w", err)
	}
	return &Client{kc: kc}, nil
}

func (c *Client) Publish(ctx context.Context, m Message) error {
	rec := &kgo.Record{Topic: m.Topic, Key: m.Key, Value: m.Value}
	if err := c.kc.ProduceSync(ctx, rec).FirstErr(); err != nil {
		return fmt.Errorf("publish to %s: %w", m.Topic, err)
	}
	return nil
}

func (c *Client) Close() error {
	c.kc.Close()
	return nil
}

// Ping reports whether the broker is reachable, for readyz.
func (c *Client) Ping(ctx context.Context) error {
	return c.kc.Ping(ctx)
}
