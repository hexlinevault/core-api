//go:build integration

// Integration tests for the RabbitMQ bootstrap. Requires a live broker and
// Redis, e.g.:
//
//	docker run -d --name rabbit -p 5673:5672 rabbitmq:4
//	docker run -d --name redis -p 6380:6379 redis:7
//	RABBITMQ_TEST_URL=amqp://guest:guest@localhost:5673/ REDIS_TEST_ADDR=localhost:6380 \
//	  go test -tags integration ./bootstrap/ -run TestRabbitIntegration -v
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hexlinevault/core-api/configs"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rabbitTestConf(name string) *configs.RabbitMQConn {
	url := os.Getenv("RABBITMQ_TEST_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5673/"
	}
	return &configs.RabbitMQConn{
		URL:              url,
		ConnectionName:   name,
		Prefix:           "it_",
		PublisherConfirm: true,
		ReconnectDelay:   500 * time.Millisecond,
	}
}

func setupTestRedis(t *testing.T) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "localhost:6380"
	}
	CreateRedisConnection(&configs.RedisConn{
		UniversalOptions: redis.UniversalOptions{Addrs: []string{addr}},
	})
}

func newRabbitService(t *testing.T, name string) *RabbitMQService {
	t.Helper()
	s := CreateRabbitMQService(rabbitTestConf(name))
	t.Cleanup(func() { s.Close() })
	return s
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func TestRabbitIntegrationPublishSubscribeTyped(t *testing.T) {
	s := newRabbitService(t, "it-pubsub")

	type betEvent struct {
		BetID  string `json:"bet_id"`
		Amount string `json:"amount"`
	}

	var mu sync.Mutex
	var got []betEvent
	s.Subscribe("bets", func(d *amqp.Delivery, data *betEvent) error {
		mu.Lock()
		got = append(got, *data)
		mu.Unlock()
		return nil
	})
	require.NoError(t, s.Start())
	time.Sleep(500 * time.Millisecond) // let the consumer declare + attach

	require.NoError(t, s.Publish(context.Background(), "bets", betEvent{BetID: "b1", Amount: "10.50"}))
	require.NoError(t, s.Publish(context.Background(), "bets", betEvent{BetID: "b2", Amount: "99.99"}))

	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	}, "2 typed messages")

	mu.Lock()
	defer mu.Unlock()
	ids := map[string]string{got[0].BetID: got[0].Amount, got[1].BetID: got[1].Amount}
	assert.Equal(t, map[string]string{"b1": "10.50", "b2": "99.99"}, ids)
}

func TestRabbitIntegrationExchangeRouting(t *testing.T) {
	s := newRabbitService(t, "it-routing")

	var settled, created atomic.Int32
	s.Subscribe("settle-worker", func(d *amqp.Delivery, data interface{}) error {
		settled.Add(1)
		return nil
	}, BindExchange("game-events", "topic", "bet.settled"))
	s.Subscribe("create-worker", func(d *amqp.Delivery, data interface{}) error {
		created.Add(1)
		return nil
	}, BindExchange("game-events", "topic", "bet.created"))
	require.NoError(t, s.Start())
	time.Sleep(500 * time.Millisecond)

	ctx := context.Background()
	require.NoError(t, s.Publish(ctx, "bet.settled", map[string]string{"id": "1"}, WithExchange("game-events")))
	require.NoError(t, s.Publish(ctx, "bet.settled", map[string]string{"id": "2"}, WithExchange("game-events")))
	require.NoError(t, s.Publish(ctx, "bet.created", map[string]string{"id": "3"}, WithExchange("game-events")))

	waitFor(t, 5*time.Second, func() bool { return settled.Load() == 2 && created.Load() == 1 }, "routed messages")
}

func TestRabbitIntegrationDedup(t *testing.T) {
	setupTestRedis(t)
	s := newRabbitService(t, "it-dedup")

	var handled atomic.Int32
	s.Subscribe("dedup-q", func(d *amqp.Delivery, data interface{}) error {
		handled.Add(1)
		return nil
	}, WithDedup(time.Minute))
	require.NoError(t, s.Start())
	time.Sleep(500 * time.Millisecond)

	ctx := context.Background()
	// Same message ID published twice — handler must run once. IDs are unique
	// per run because dedup keys from a previous run live in Redis for DedupTTL.
	runID := uuid.NewString()
	require.NoError(t, s.Publish(ctx, "dedup-q", "payload", WithMessageID(runID+"-dup-1")))
	require.NoError(t, s.Publish(ctx, "dedup-q", "payload", WithMessageID(runID+"-dup-1")))
	require.NoError(t, s.Publish(ctx, "dedup-q", "payload", WithMessageID(runID+"-dup-2")))

	waitFor(t, 5*time.Second, func() bool { return handled.Load() >= 2 }, "deduped messages")
	time.Sleep(time.Second) // give a would-be duplicate time to arrive
	assert.Equal(t, int32(2), handled.Load(), "duplicate message id must be processed once")
}

func TestRabbitIntegrationDeadLetterAndRetry(t *testing.T) {
	s := newRabbitService(t, "it-dlq")

	var attempts atomic.Int32
	s.Subscribe("poison-q", func(d *amqp.Delivery, data interface{}) error {
		attempts.Add(1)
		return fmt.Errorf("always fails")
	}, WithDeadLetter(), WithConsumerMaxRetry(2), WithConsumerRetryDelay(10*time.Millisecond))
	require.NoError(t, s.Start())
	time.Sleep(500 * time.Millisecond)

	// Drop leftovers from previous runs so the DLQ count below is exact.
	purgeCh, err := s.Channel()
	require.NoError(t, err)
	_, err = purgeCh.QueuePurge("it_poison-q.dlq", false)
	require.NoError(t, err)
	purgeCh.Close()

	require.NoError(t, s.Publish(context.Background(), "poison-q", "bad message"))

	// 1 initial + 2 retries, then dead-lettered
	waitFor(t, 5*time.Second, func() bool { return attempts.Load() == 3 }, "3 handler attempts")

	ch, err := s.Channel()
	require.NoError(t, err)
	defer ch.Close()
	waitFor(t, 5*time.Second, func() bool {
		q, err := ch.QueueDeclarePassive("it_poison-q.dlq", true, false, false, false, nil)
		return err == nil && q.Messages == 1
	}, "message in DLQ")
}

func TestRabbitIntegrationClusterLockFailover(t *testing.T) {
	setupTestRedis(t)

	// Two service instances simulate two clusters sharing one Redis.
	a := newRabbitService(t, "it-cluster-a")
	b := newRabbitService(t, "it-cluster-b")

	var fromA, fromB atomic.Int32
	lockOpts := []RabbitSubscribeOption{
		WithClusterLock("it:lock:cluster-q"),
		WithClusterLockTTL(2 * time.Second),
	}
	a.Subscribe("cluster-q", func(d *amqp.Delivery, data interface{}) error {
		fromA.Add(1)
		return nil
	}, lockOpts...)
	require.NoError(t, a.Start())

	// Let A win the election before B joins.
	time.Sleep(time.Second)

	b.Subscribe("cluster-q", func(d *amqp.Delivery, data interface{}) error {
		fromB.Add(1)
		return nil
	}, lockOpts...)
	require.NoError(t, b.Start())
	time.Sleep(time.Second)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, a.Publish(ctx, "cluster-q", fmt.Sprintf("m%d", i)))
	}
	waitFor(t, 5*time.Second, func() bool { return fromA.Load() == 5 }, "leader A consumed all")
	assert.Equal(t, int32(0), fromB.Load(), "standby B must not consume while A holds the lock")

	// Leader goes down → B must take over within ~TTL.
	a.Close()
	waitFor(t, 10*time.Second, func() bool {
		if err := b.Publish(ctx, "cluster-q", "after-failover"); err != nil {
			return false
		}
		time.Sleep(200 * time.Millisecond)
		return fromB.Load() > 0
	}, "B took over after A stopped")
}

// TestRabbitIntegrationReconnect restarts the broker mid-run and verifies both
// the consumer and the publisher survive. Needs a restart command, e.g.:
//
//	RABBITMQ_TEST_RESTART_CMD="docker restart <container>" go test -tags integration ...
func TestRabbitIntegrationReconnect(t *testing.T) {
	restartCmd := os.Getenv("RABBITMQ_TEST_RESTART_CMD")
	if restartCmd == "" {
		t.Skip("RABBITMQ_TEST_RESTART_CMD not set")
	}

	s := newRabbitService(t, "it-reconnect")

	var handled atomic.Int32
	s.Subscribe("reconnect-q", func(d *amqp.Delivery, data interface{}) error {
		handled.Add(1)
		return nil
	})
	require.NoError(t, s.Start())
	time.Sleep(500 * time.Millisecond)

	ctx := context.Background()
	require.NoError(t, s.Publish(ctx, "reconnect-q", "before-restart"))
	waitFor(t, 5*time.Second, func() bool { return handled.Load() == 1 }, "message before restart")

	out, err := exec.Command("sh", "-c", restartCmd).CombinedOutput()
	require.NoError(t, err, string(out))

	// Publish retries through the reconnect loop; the consumer re-attaches on
	// its own. Keep trying until the broker is back.
	waitFor(t, 60*time.Second, func() bool {
		pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return s.Publish(pubCtx, "reconnect-q", "after-restart") == nil
	}, "publish succeeded after broker restart")

	waitFor(t, 30*time.Second, func() bool { return handled.Load() == 2 }, "message consumed after broker restart")
}
