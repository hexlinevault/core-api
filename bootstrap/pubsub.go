package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.elastic.co/apm"
)

const defaultQueueName = "default"

var PubSub *PubSubService

var (
	errType = reflect.TypeOf((*error)(nil)).Elem()
	msgType = reflect.TypeOf((*PubSubMessage)(nil))
)

// PubSubMessage is the message delivered to a subscriber, analogous to
// sarama.ConsumerMessage on the Kafka side. Context carries the per-message
// context (with the APM transaction); Payload is the raw JSON bytes.
type PubSubMessage struct {
	Context context.Context
	Topic   string
	Payload []byte
	ID      string // asynq task ID
}

// HandlerFunc is the raw callback for a topic. Payload is the raw JSON bytes
// from Publish. Kept for backward compatibility; new code can use the Kafka-style
// func(*PubSubMessage, *T) error handlers (see Subscribe).
type HandlerFunc func(ctx context.Context, payload []byte) error

// msgHandler is the internal, normalized handler shape stored per topic.
type msgHandler func(msg *PubSubMessage) error

type PubSubService struct {
	client    *asynq.Client
	rdb       redis.UniversalClient
	queueName string
	prefix    string
	server    *asynq.Server
	handlers  map[string]msgHandler
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
		handlers:  make(map[string]msgHandler),
	}
}

// Subscribe registers a handler for a topic. Call before Start().
//
// handler may be any of these shapes (mirrors the Kafka service):
//   - func(msg *PubSubMessage, data *T) error  — JSON payload auto-unmarshaled into *T
//   - func(msg *PubSubMessage, payload []byte) error  — raw bytes, message metadata available
//   - func(ctx context.Context, payload []byte) error — raw bytes (legacy, ctx only)
//
// For the Kafka-style forms, msg.Topic / msg.Context / msg.Payload are available
// inside the handler.
func (s *PubSubService) Subscribe(topic string, handler interface{}) {
	if topic == "" {
		panic("pubsub: topic is required")
	}
	if handler == nil {
		panic("pubsub: handler is required")
	}
	if s.prefix != "" {
		topic = s.prefix + topic
	}
	fn := s.wrapHandler(handler)
	s.mu.Lock()
	s.handlers[topic] = fn
	s.mu.Unlock()

	Logger(context.Background()).
		WithField("topic", topic).
		WithField("queue", s.queueName).
		WithField("component", "pubsub").
		Infof("Registered handler for topic %s", topic)
}

// wrapHandler normalizes a user handler into the internal msgHandler shape.
func (s *PubSubService) wrapHandler(handler interface{}) msgHandler {
	switch h := handler.(type) {
	case HandlerFunc:
		return func(m *PubSubMessage) error { return h(m.Context, m.Payload) }
	case func(context.Context, []byte) error:
		return func(m *PubSubMessage) error { return h(m.Context, m.Payload) }
	case func(*PubSubMessage, []byte) error:
		return func(m *PubSubMessage) error { return h(m, m.Payload) }
	}

	// Reflection path: func(*PubSubMessage, *T) error
	v := reflect.ValueOf(handler)
	t := v.Type()
	if t.Kind() != reflect.Func || t.NumIn() != 2 || t.NumOut() != 1 {
		panic(fmt.Sprintf("pubsub subscribe: handler must be func(*PubSubMessage, *T) error, got %T", handler))
	}
	if t.In(0) != msgType {
		panic(fmt.Sprintf("pubsub subscribe: first arg must be *PubSubMessage or context.Context, got %v", t.In(0)))
	}
	if !t.Out(0).Implements(errType) {
		panic(fmt.Sprintf("pubsub subscribe: handler must return error, got %v", t.Out(0)))
	}
	arg1 := t.In(1)
	if arg1.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("pubsub subscribe: second arg must be a pointer (*T), got %v", arg1))
	}

	return func(m *PubSubMessage) error {
		ptr := reflect.New(arg1.Elem()) // new(T) -> *T
		if len(m.Payload) > 0 {
			if err := json.Unmarshal(m.Payload, ptr.Interface()); err != nil {
				return err
			}
		}
		res := v.Call([]reflect.Value{reflect.ValueOf(m), ptr})
		if res[0].IsNil() {
			return nil
		}
		return res[0].Interface().(error)
	}
}

func (s *PubSubService) processTask(ctx context.Context, task *asynq.Task) error {
	topic := task.Type()
	s.mu.RLock()
	fn, ok := s.handlers[topic]
	s.mu.RUnlock()
	if !ok {
		Logger(ctx).
			WithField("topic", topic).
			WithField("component", "pubsub").
			Warn("No handler for topic, discarding message")
		return nil
	}

	taskID, _ := asynq.GetTaskID(ctx)

	tracer := apm.DefaultTracer
	if !tracer.Recording() {
		return fn(&PubSubMessage{Context: ctx, Topic: topic, Payload: task.Payload(), ID: taskID})
	}

	tx := tracer.StartTransaction(fmt.Sprintf("PubSub Consume %s", topic), "messaging")
	defer tx.End()
	tx.Context.SetLabel("topic", topic)
	tx.Context.SetLabel("queue", s.queueName)
	ctx = apm.ContextWithTransaction(ctx, tx)

	defer func() {
		if r := recover(); r != nil {
			e := tracer.Recovered(r)
			e.SetTransaction(tx)
			e.Send()
			panic(r)
		}
	}()

	err := fn(&PubSubMessage{Context: ctx, Topic: topic, Payload: task.Payload(), ID: taskID})
	if err != nil {
		tx.Result = "failure"
		e := tracer.NewError(err)
		e.SetTransaction(tx)
		e.Send()
		return err
	}
	tx.Result = "success"
	return nil
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
