package bootstrap

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const defaultQueueName = "default"

var PubSub *PubSubService

// HandlerFunc is the callback for a topic. Payload is the raw JSON bytes from Publish.
type HandlerFunc func(ctx context.Context, payload []byte) error

type PubSubService struct {
	client    *asynq.Client
	rdb       redis.UniversalClient
	queueName string
	prefix    string
	server    *asynq.Server
	handlers  map[string]HandlerFunc
	mu        sync.RWMutex
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func CreatePubSubService(redisConnectionName string, prefix ...string) {
	rd := dbRedis[redisConnectionName]
	if rd == nil {
		panic("redis connection not found")
	}
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	PubSub = NewPubSubService(rd, defaultQueueName, p)
}

// NewPubSubService creates a PubSubService from a Redis client and queue name.
// If queueName is empty, defaultQueueName is used. Useful for tests with isolated queues.
func NewPubSubService(rdb redis.UniversalClient, queueName string, prefix ...string) *PubSubService {
	if rdb == nil {
		panic("pubsub: redis client is nil")
	}
	if queueName == "" {
		queueName = defaultQueueName
	}
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	return &PubSubService{
		client:    asynq.NewClientFromRedisClient(rdb),
		rdb:       rdb,
		queueName: queueName,
		prefix:    p,
		handlers:  make(map[string]HandlerFunc),
	}
}

// Subscribe registers a handler for a topic. Call before Start().
func (s *PubSubService) Subscribe(topic string, fn HandlerFunc) {
	if topic == "" {
		panic("pubsub: topic is required")
	}
	if fn == nil {
		panic("pubsub: handler is required")
	}
	if s.prefix != "" {
		topic = s.prefix + topic
	}
	s.mu.Lock()
	s.handlers[topic] = fn
	s.mu.Unlock()

	Logger(context.Background()).
		WithField("topic", topic).
		WithField("queue", s.queueName).
		WithField("component", "pubsub").
		Infof("Registered handler for topic %s", topic)
}

func (s *PubSubService) processTask(ctx context.Context, task *asynq.Task) error {
	topic := task.Type()
	payload := task.Payload()
	s.mu.RLock()
	fn, ok := s.handlers[topic]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return fn(ctx, payload)
}

// Start starts the worker in a background goroutine. Call after Subscribe. Safe to call once.
func (s *PubSubService) Start() {
	if s.server == nil {
		s.server = asynq.NewServerFromRedisClient(s.rdb, asynq.Config{
			Queues:                   map[string]int{s.queueName: 1},
			Concurrency:              10,
			DelayedTaskCheckInterval: 500 * time.Millisecond,
		})
	}

	s.mu.RLock()
	topics := make([]string, 0, len(s.handlers))
	for topic := range s.handlers {
		topics = append(topics, topic)
	}
	s.mu.RUnlock()
	Logger(context.Background()).
		WithField("queue", s.queueName).
		WithField("topics", topics).
		WithField("component", "pubsub").
		Infof("Starting consumer with %d topic(s)", len(topics))

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.server.Start(asynq.HandlerFunc(s.processTask))
	}()
}

// Close gracefully stops the worker and closes the client. Idempotent.
func (s *PubSubService) Close() {
	s.closeOnce.Do(func() {
		if s.server != nil {
			s.server.Shutdown()
		}
		_ = s.client.Close()
		s.wg.Wait()
	})
}

func (s *PubSubService) Publish(ctx context.Context, topic string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if s.prefix != "" {
		topic = s.prefix + topic
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	task := asynq.NewTask(topic, payloadBytes)
	opts = append([]asynq.Option{asynq.Queue(s.queueName)}, opts...)
	return s.client.EnqueueContext(ctx, task, opts...)
}
