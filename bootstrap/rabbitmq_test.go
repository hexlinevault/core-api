package bootstrap

import (
	"fmt"
	"testing"
	"time"

	"github.com/hexlinevault/core-api/configs"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

func newTestRabbitService() *RabbitMQService {
	return &RabbitMQService{config: &configs.RabbitMQConn{Prefix: "test_"}}
}

func TestRabbitMQConnURLs(t *testing.T) {
	conf := &configs.RabbitMQConn{URL: "amqp://u:p@host:5672/vh"}
	assert.Equal(t, []string{"amqp://u:p@host:5672/vh"}, conf.URLs())

	conf = &configs.RabbitMQConn{
		Hosts:    []string{"node1:5672", "node2:5672"},
		Username: "user",
		Password: "pa:ss@word",
		VHost:    "casino",
	}
	urls := conf.URLs()
	assert.Len(t, urls, 2)
	assert.Equal(t, "amqp://user:pa%3Ass%40word@node1:5672/casino", urls[0])
	assert.Equal(t, "amqp://user:pa%3Ass%40word@node2:5672/casino", urls[1])

	// default vhost
	conf = &configs.RabbitMQConn{Hosts: []string{"node1:5672"}, Username: "u", Password: "p", VHost: "/"}
	assert.Equal(t, "amqp://u:p@node1:5672/", conf.URLs()[0])
}

func TestRabbitWrapHandlerInterface(t *testing.T) {
	s := newTestRabbitService()

	var received interface{}
	handler, targetType := s.wrapHandler(func(d *amqp.Delivery, data interface{}) error {
		received = data
		return nil
	})
	assert.Nil(t, targetType)

	err := handler(&amqp.Delivery{}, map[string]interface{}{"a": float64(1)})
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"a": float64(1)}, received)
}

func TestRabbitWrapHandlerTypedStruct(t *testing.T) {
	type betEvent struct {
		BetID  string `json:"bet_id"`
		Amount string `json:"amount"`
	}

	s := newTestRabbitService()

	var received *betEvent
	handler, targetType := s.wrapHandler(func(d *amqp.Delivery, data *betEvent) error {
		received = data
		return nil
	})
	assert.NotNil(t, targetType)

	data := decodeRabbitPayload(targetType, []byte(`{"bet_id":"b1","amount":"10.50"}`))
	err := handler(&amqp.Delivery{}, data)
	assert.NoError(t, err)
	assert.Equal(t, "b1", received.BetID)
	assert.Equal(t, "10.50", received.Amount)
}

func TestRabbitWrapHandlerPropagatesError(t *testing.T) {
	s := newTestRabbitService()

	handler, _ := s.wrapHandler(func(d *amqp.Delivery, data interface{}) error {
		return fmt.Errorf("boom")
	})
	err := handler(&amqp.Delivery{}, "x")
	assert.EqualError(t, err, "boom")
}

func TestRabbitWrapHandlerRejectsBadSignature(t *testing.T) {
	s := newTestRabbitService()

	assert.Panics(t, func() { s.wrapHandler(func() {}) })
	assert.Panics(t, func() { s.wrapHandler("not a func") })
	assert.Panics(t, func() { s.wrapHandler(func(a string, b interface{}) error { return nil }) })
}

func TestDecodeRabbitPayloadFallbacks(t *testing.T) {
	// interface{} target, invalid JSON → raw bytes
	data := decodeRabbitPayload(nil, []byte("not-json"))
	assert.Equal(t, []byte("not-json"), data)

	// interface{} target, valid JSON
	data = decodeRabbitPayload(nil, []byte(`{"k":"v"}`))
	assert.Equal(t, map[string]interface{}{"k": "v"}, data)
}

func TestRabbitSubscribeOptions(t *testing.T) {
	opts := &rabbitSubscribeOptions{
		ExchangeKind: "topic",
		Prefetch:     10,
		Concurrency:  1,
		RetryDelay:   time.Second,
		LockTTL:      15 * time.Second,
	}
	for _, o := range []RabbitSubscribeOption{
		BindExchange("game-events", "direct", "bet.created", "bet.settled"),
		WithPrefetch(50),
		WithConcurrency(8),
		WithConsumerMaxRetry(3),
		WithConsumerRetryDelay(2 * time.Second),
		WithDeadLetter(),
		WithQuorumQueue(),
		WithSingleActiveConsumer(),
		WithClusterLock("rabbitmq:lock:bet-consumer"),
		WithClusterLockTTL(30 * time.Second),
		WithLockRedisConnection("locks"),
		WithDedup(time.Hour),
	} {
		o(opts)
	}

	assert.Equal(t, "game-events", opts.Exchange)
	assert.Equal(t, "direct", opts.ExchangeKind)
	assert.Equal(t, []string{"bet.created", "bet.settled"}, opts.RoutingKeys)
	assert.Equal(t, 50, opts.Prefetch)
	assert.Equal(t, 8, opts.Concurrency)
	assert.Equal(t, 3, opts.MaxRetry)
	assert.Equal(t, 2*time.Second, opts.RetryDelay)
	assert.True(t, opts.DeadLetter)
	assert.True(t, opts.Quorum)
	assert.True(t, opts.SingleActiveConsumer)
	assert.Equal(t, "rabbitmq:lock:bet-consumer", opts.LockKey)
	assert.Equal(t, 30*time.Second, opts.LockTTL)
	assert.Equal(t, "locks", opts.LockConnection)
	assert.Equal(t, "locks", opts.DedupConnection)
	assert.Equal(t, time.Hour, opts.DedupTTL)
}

func TestRabbitPublishOptions(t *testing.T) {
	opts := &rabbitPublishOptions{}
	for _, o := range []RabbitPublishOption{
		WithExchange("game-events"),
		WithHeader("x-source", "api-service"),
		WithPriority(5),
		WithMessageTTL(30 * time.Second),
		WithMessageID("msg-123"),
		WithTransient(),
		WithMandatory(),
	} {
		o(opts)
	}

	assert.Equal(t, "game-events", opts.Exchange)
	assert.Equal(t, "api-service", opts.Headers["x-source"])
	assert.Equal(t, uint8(5), opts.Priority)
	assert.Equal(t, 30*time.Second, opts.Expiration)
	assert.Equal(t, "msg-123", opts.MessageID)
	assert.True(t, opts.Transient)
	assert.True(t, opts.Mandatory)
}

func TestRabbitPrefixAndDedupKey(t *testing.T) {
	s := newTestRabbitService()

	assert.Equal(t, "test_bets", s.prefixed("bets"))
	assert.Equal(t, "", s.prefixed(""))

	// dedup key uses MessageId when present
	key := s.dedupKey("test_bets", &amqp.Delivery{MessageId: "m1"})
	assert.Equal(t, "rabbitmq:dedup:test_bets:m1", key)

	// falls back to body hash, stable across calls
	d := &amqp.Delivery{Body: []byte(`{"bet_id":"b1"}`)}
	assert.Equal(t, s.dedupKey("test_bets", d), s.dedupKey("test_bets", d))
	assert.NotEqual(t, s.dedupKey("test_bets", d), s.dedupKey("test_bets", &amqp.Delivery{Body: []byte("other")}))
}
