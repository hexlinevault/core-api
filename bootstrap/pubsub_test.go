package bootstrap_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hexlinevault/core-api/bootstrap"
)

func testPubSubRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	rdb := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{"localhost:6379"},
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skipping: Redis not available at localhost:6379 (%v)", err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func newTestPubSub(t *testing.T) *bootstrap.PubSubService {
	t.Helper()
	rdb := testPubSubRedis(t)
	key := "test:pubsub:" + t.Name()
	return bootstrap.NewPubSubService(rdb, key)
}

func startPubSubWorker(t *testing.T, s *bootstrap.PubSubService) {
	t.Helper()
	s.Start()
	t.Cleanup(s.Close)
}

func TestPubSub_ImmediateDispatch(t *testing.T) {
	s := newTestPubSub(t)
	type Msg struct{ Text string }
	received := make(chan string, 1)
	s.Subscribe("topic", func(ctx context.Context, payload []byte) error {
		var m Msg
		require.NoError(t, json.Unmarshal(payload, &m))
		received <- m.Text
		return nil
	})
	startPubSubWorker(t, s)

	_, err := s.Publish(context.Background(), "topic", Msg{Text: "hello"})
	require.NoError(t, err)

	select {
	case v := <-received:
		assert.Equal(t, "hello", v)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for immediate message")
	}
}

func TestPubSub_DelayedDispatch(t *testing.T) {
	s := newTestPubSub(t)
	var deliveredAt time.Time
	var mu sync.Mutex
	done := make(chan struct{})
	s.Subscribe("topic", func(ctx context.Context, payload []byte) error {
		mu.Lock()
		deliveredAt = time.Now()
		mu.Unlock()
		close(done)
		return nil
	})
	startPubSubWorker(t, s)

	const delay = 600 * time.Millisecond
	publishedAt := time.Now()
	_, err := s.Publish(context.Background(), "topic", struct{}{}, asynq.ProcessIn(delay))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for delayed message")
	}
	mu.Lock()
	elapsed := deliveredAt.Sub(publishedAt)
	mu.Unlock()
	assert.GreaterOrEqual(t, elapsed, delay, "message delivered before delay expired")
}

func TestPubSub_UnknownTopicDiscarded(t *testing.T) {
	s := newTestPubSub(t)
	marker := make(chan struct{})
	s.Subscribe("marker", func(ctx context.Context, payload []byte) error {
		close(marker)
		return nil
	})
	startPubSubWorker(t, s)

	_, _ = s.Publish(context.Background(), "ghost", struct{}{})
	_, _ = s.Publish(context.Background(), "marker", struct{}{})

	select {
	case <-marker:
	case <-time.After(3 * time.Second):
		t.Fatal("worker stopped after unknown-topic message")
	}
}

func TestPubSub_NoDuplicates(t *testing.T) {
	const msgCount = 20
	rdb := testPubSubRedis(t)
	key := "test:pubsub:" + t.Name()

	var processed sync.Map
	var total atomic.Int32
	var wg sync.WaitGroup
	wg.Add(msgCount)

	handler := func(ctx context.Context, payload []byte) error {
		var id int
		require.NoError(t, json.Unmarshal(payload, &id))
		if _, loaded := processed.LoadOrStore(id, true); loaded {
			t.Errorf("message %d processed more than once", id)
		}
		total.Add(1)
		wg.Done()
		return nil
	}

	w1 := bootstrap.NewPubSubService(rdb, key)
	w2 := bootstrap.NewPubSubService(rdb, key)
	w1.Subscribe("item", handler)
	w2.Subscribe("item", handler)
	t.Cleanup(w1.Close)
	t.Cleanup(w2.Close)
	w1.Start()
	w2.Start()

	pub := bootstrap.NewPubSubService(rdb, key)
	for i := 0; i < msgCount; i++ {
		_, err := pub.Publish(context.Background(), "item", i)
		require.NoError(t, err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatalf("timed out; %d/%d processed", total.Load(), msgCount)
	}
	assert.Equal(t, int32(msgCount), total.Load())
}

func TestPubSub_Close_Idempotent(t *testing.T) {
	s := newTestPubSub(t)
	s.Subscribe("t", func(ctx context.Context, payload []byte) error { return nil })
	s.Start()
	s.Close()
	s.Close()
	s.Close()
}
