package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/hexlinevault/core-api/configs"

	"github.com/go-redsync/redsync/v4"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.elastic.co/apm"
)

var Rabbit *RabbitMQService

// RabbitHandler is the callback function type for processing RabbitMQ messages.
// Returns error to trigger retry (if WithConsumerMaxRetry > 0).
type RabbitHandler func(d *amqp.Delivery, data interface{}) error

// RabbitMQService wraps a single AMQP connection with auto-reconnect,
// a confirmed publisher channel, and supervised consumers.
//
// Multi-cluster coordination:
//   - WithSingleActiveConsumer() — broker-side: only one consumer on the queue
//     is active at a time (works within one RabbitMQ cluster).
//   - WithClusterLock(key) — Redis (redsync) leader election: only the lock
//     holder consumes, across ANY number of app clusters sharing Redis.
//   - WithDedup(ttl) — per-message idempotency guard via Redis SETNX, protects
//     against double-processing on redelivery or overlapping consumers.
type RabbitMQService struct {
	config *configs.RabbitMQConn

	ctx    context.Context
	cancel context.CancelFunc

	connMu sync.Mutex
	conn   *amqp.Connection

	pubMu sync.Mutex
	pubCh *amqp.Channel

	subsMu    sync.Mutex
	subs      []*rabbitSubscription
	started   bool
	startedMu sync.Mutex

	wg        sync.WaitGroup // consumer supervisor goroutines
	messageWg sync.WaitGroup // in-flight messages
}

type rabbitSubscription struct {
	queue      string // raw name, prefix applied at declare time
	handler    RabbitHandler
	targetType reflect.Type
	opts       *rabbitSubscribeOptions
}

type (
	RabbitPublishOption func(*rabbitPublishOptions)

	rabbitPublishOptions struct {
		Exchange   string
		Headers    amqp.Table
		Priority   uint8
		Expiration time.Duration
		MessageID  string
		Transient  bool
		Mandatory  bool
	}
)

// WithExchange publishes to a named exchange instead of the default exchange.
func WithExchange(exchange string) RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.Exchange = exchange }
}

// WithHeader adds a message header.
func WithHeader(key string, value interface{}) RabbitPublishOption {
	return func(o *rabbitPublishOptions) {
		if o.Headers == nil {
			o.Headers = amqp.Table{}
		}
		o.Headers[key] = value
	}
}

// WithPriority sets message priority (0-9, requires x-max-priority on the queue).
func WithPriority(priority uint8) RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.Priority = priority }
}

// WithMessageTTL sets per-message expiration; expired messages are dropped or
// dead-lettered by the broker.
func WithMessageTTL(ttl time.Duration) RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.Expiration = ttl }
}

// WithMessageID sets an explicit message ID (also used as the dedup key by WithDedup).
func WithMessageID(id string) RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.MessageID = id }
}

// WithTransient marks the message non-persistent (lost on broker restart).
func WithTransient() RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.Transient = true }
}

// WithMandatory returns the message to the publisher if it cannot be routed.
func WithMandatory() RabbitPublishOption {
	return func(o *rabbitPublishOptions) { o.Mandatory = true }
}

type (
	RabbitSubscribeOption func(*rabbitSubscribeOptions)

	rabbitSubscribeOptions struct {
		Exchange             string
		ExchangeKind         string
		RoutingKeys          []string
		Prefetch             int
		Concurrency          int
		MaxRetry             int
		RetryDelay           time.Duration
		RequeueOnFailure     bool
		DeadLetter           bool
		AutoDelete           bool
		Quorum               bool
		SingleActiveConsumer bool
		Passive              bool
		QueueArgs            amqp.Table

		LockKey        string
		LockTTL        time.Duration
		LockConnection string

		DedupTTL        time.Duration
		DedupConnection string
	}
)

// BindExchange declares the exchange (topic/direct/fanout/headers) and binds
// the queue to it with the given routing keys.
func BindExchange(exchange, kind string, routingKeys ...string) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) {
		o.Exchange = exchange
		o.ExchangeKind = kind
		o.RoutingKeys = routingKeys
	}
}

// WithPrefetch overrides the per-consumer QoS prefetch count.
func WithPrefetch(n int) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.Prefetch = n }
}

// WithConcurrency sets the number of worker goroutines per subscription (default 1).
func WithConcurrency(n int) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.Concurrency = n }
}

// WithConsumerMaxRetry sets in-process retry attempts when the handler returns
// an error (default 0 = no retry).
func WithConsumerMaxRetry(maxRetry int) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.MaxRetry = maxRetry }
}

// WithConsumerRetryDelay sets the delay between retry attempts (default 1s).
func WithConsumerRetryDelay(delay time.Duration) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.RetryDelay = delay }
}

// WithRequeueOnFailure requeues the message when all retries fail instead of
// dropping/dead-lettering it. Beware of poison-message loops.
func WithRequeueOnFailure() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.RequeueOnFailure = true }
}

// WithDeadLetter declares <queue>.dlx + <queue>.dlq and routes failed
// (nacked / expired) messages there.
func WithDeadLetter() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.DeadLetter = true }
}

// WithAutoDelete deletes the queue when the last consumer disconnects.
func WithAutoDelete() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.AutoDelete = true }
}

// WithQuorumQueue declares the queue as a quorum queue (replicated, for
// RabbitMQ clusters).
func WithQuorumQueue() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.Quorum = true }
}

// WithSingleActiveConsumer enables the broker-native x-single-active-consumer
// flag: all instances stay connected but only one receives messages; the
// broker fails over automatically. Use this for ordering guarantees within a
// single RabbitMQ cluster.
func WithSingleActiveConsumer() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.SingleActiveConsumer = true }
}

// WithQueueArgs merges extra queue declaration arguments (e.g. x-max-priority).
func WithQueueArgs(args amqp.Table) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.QueueArgs = args }
}

// WithPassiveQueue skips all topology declaration and consumes from a
// pre-existing queue. Use when connecting to an external broker where we lack
// configure permission (e.g. provider feeds like LSports). The queue name is
// used as-is apart from the config Prefix — external connections should set
// Prefix to "".
func WithPassiveQueue() RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.Passive = true }
}

// WithClusterLock runs the consumer under a redsync distributed lock: across
// every app cluster sharing the same Redis, only the current lock holder
// consumes the queue. If the holder dies or loses the lock, another instance
// takes over within ~ttl. Use this when multiple k8s clusters run the same
// consumer and only one may be active (same mechanism as StartCronLeaderElection).
func WithClusterLock(lockKey string) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.LockKey = lockKey }
}

// WithClusterLockTTL overrides the lock expiry (default 15s; failover time is
// bounded by this value).
func WithClusterLockTTL(ttl time.Duration) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.LockTTL = ttl }
}

// WithLockRedisConnection selects the named Redis connection for the cluster
// lock and dedup (default connection otherwise).
func WithLockRedisConnection(name string) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) {
		o.LockConnection = name
		o.DedupConnection = name
	}
}

// WithDedup skips messages whose ID was already processed within ttl, using
// Redis SETNX. The key is the message's MessageId (set via WithMessageID on
// publish) or a hash of the body. This is the idempotency guard for
// at-least-once delivery and overlapping consumers across clusters.
func WithDedup(ttl time.Duration) RabbitSubscribeOption {
	return func(o *rabbitSubscribeOptions) { o.DedupTTL = ttl }
}

// CreateRabbitMQService creates the RabbitMQ service and verifies connectivity.
func CreateRabbitMQService(conf *configs.RabbitMQConn) *RabbitMQService {
	if conf.PrefetchCount <= 0 {
		conf.PrefetchCount = 10
	}
	if conf.ReconnectDelay <= 0 {
		conf.ReconnectDelay = 2 * time.Second
	}
	if conf.PublishTimeout <= 0 {
		conf.PublishTimeout = 10 * time.Second
	}
	if conf.Heartbeat <= 0 {
		conf.Heartbeat = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &RabbitMQService{
		config: conf,
		ctx:    ctx,
		cancel: cancel,
	}

	if _, err := s.dial(); err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "rabbitmq").Fatal("Error connecting to RabbitMQ")
	}
	Logger(context.Background()).WithField("component", "rabbitmq").Info("RabbitMQ connected")

	Rabbit = s
	return s
}

// dial tries every configured URL once and stores the first live connection.
// Caller must hold no locks; connMu is taken inside.
func (s *RabbitMQService) dial() (*amqp.Connection, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.dialLocked()
}

func (s *RabbitMQService) dialLocked() (*amqp.Connection, error) {
	var lastErr error
	for _, u := range s.config.URLs() {
		conn, err := amqp.DialConfig(u, amqp.Config{
			Heartbeat: s.config.Heartbeat,
			Properties: amqp.Table{
				"connection_name": s.config.ConnectionName,
			},
		})
		if err != nil {
			lastErr = err
			continue
		}
		s.conn = conn
		return conn, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("rabbitmq: no hosts configured")
	}
	return nil, lastErr
}

// connection returns a live connection, reconnecting with exponential backoff
// until success, caller cancellation, or shutdown.
func (s *RabbitMQService) connection(ctx context.Context) (*amqp.Connection, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn != nil && !s.conn.IsClosed() {
		return s.conn, nil
	}

	delay := s.config.ReconnectDelay
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := s.dialLocked()
		if err == nil {
			Logger(context.Background()).WithField("component", "rabbitmq").Info("RabbitMQ reconnected")
			return conn, nil
		}
		Logger(context.Background()).WithError(err).WithField("retry_in", delay).WithField("component", "rabbitmq").Warn("RabbitMQ reconnect failed")

		select {
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > 30*time.Second {
			delay = 30 * time.Second
		}
	}
}

func (s *RabbitMQService) prefixed(name string) string {
	if name == "" {
		return name
	}
	return s.config.Prefix + name
}

// Channel opens a raw AMQP channel on the managed connection for advanced use
// (custom topology, transactions). The caller owns and must close it.
func (s *RabbitMQService) Channel() (*amqp.Channel, error) {
	conn, err := s.connection(context.Background())
	if err != nil {
		return nil, err
	}
	return conn.Channel()
}

// DeclareExchange declares a durable exchange (topic/direct/fanout/headers) so
// publishers can publish before any consumer has bound to it.
func (s *RabbitMQService) DeclareExchange(name, kind string) error {
	ch, err := s.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	return ch.ExchangeDeclare(s.prefixed(name), kind, true, false, false, false, nil)
}

// Publish publishes a message. With the default exchange the routing key is
// the queue name; with WithExchange it is the routing pattern. Data is
// marshaled like Kafka.Publish (string / []byte passthrough, JSON otherwise).
// If PublisherConfirm is enabled, this blocks until the broker acks.
func (s *RabbitMQService) Publish(ctx context.Context, routingKey string, data interface{}, options ...RabbitPublishOption) error {
	opts := &rabbitPublishOptions{}
	for _, o := range options {
		o(opts)
	}

	if routingKey == "" {
		return fmt.Errorf("rabbitmq publish: routing key is required")
	}

	exchange := s.prefixed(opts.Exchange)
	if opts.Exchange == "" {
		// Default exchange routes directly to the queue named by the routing
		// key, so the queue prefix must be applied to it.
		routingKey = s.prefixed(routingKey)
	}

	var body []byte
	contentType := "application/json"
	switch v := data.(type) {
	case string:
		body = []byte(v)
		contentType = "text/plain"
	case []byte:
		body = v
		contentType = "application/octet-stream"
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("rabbitmq publish: failed to marshal data: %w", err)
		}
		body = bytes
	}

	headers := opts.Headers
	if headers == nil {
		headers = amqp.Table{}
	}

	// Inject APM trace context (same W3C traceparent format as Kafka.Publish)
	if tx := apm.TransactionFromContext(ctx); tx != nil {
		traceContext := tx.TraceContext()
		if traceContext.Trace.Validate() == nil {
			traceID := hex.EncodeToString(traceContext.Trace[:])
			spanID := hex.EncodeToString(traceContext.Span[:])
			flags := fmt.Sprintf("%02x", traceContext.Options)
			headers["elastic-apm-traceparent"] = fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags)
		}
	}

	deliveryMode := amqp.Persistent
	if opts.Transient {
		deliveryMode = amqp.Transient
	}

	msg := amqp.Publishing{
		ContentType:  contentType,
		DeliveryMode: deliveryMode,
		Timestamp:    time.Now(),
		MessageId:    opts.MessageID,
		Priority:     opts.Priority,
		Headers:      headers,
		Body:         body,
	}
	if opts.Expiration > 0 {
		msg.Expiration = fmt.Sprintf("%d", opts.Expiration.Milliseconds())
	}

	// One retry after a transparent reconnect: the first attempt may fail on a
	// channel that died since the last publish.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := s.publishOnce(ctx, exchange, routingKey, opts.Mandatory, msg); err != nil {
			lastErr = err
			s.resetPublisherChannel()
			continue
		}
		return nil
	}
	return fmt.Errorf("rabbitmq publish: %w", lastErr)
}

func (s *RabbitMQService) publishOnce(ctx context.Context, exchange, routingKey string, mandatory bool, msg amqp.Publishing) error {
	s.pubMu.Lock()
	ch, err := s.publisherChannelLocked(ctx)
	if err != nil {
		s.pubMu.Unlock()
		return err
	}

	if !s.config.PublisherConfirm {
		err = ch.PublishWithContext(ctx, exchange, routingKey, mandatory, false, msg)
		s.pubMu.Unlock()
		return err
	}

	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, mandatory, false, msg)
	s.pubMu.Unlock()
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, s.config.PublishTimeout)
	defer cancel()
	acked, err := confirm.WaitContext(waitCtx)
	if err != nil {
		return fmt.Errorf("waiting for publisher confirm: %w", err)
	}
	if !acked {
		return fmt.Errorf("broker nacked message (routing_key=%s)", routingKey)
	}
	return nil
}

// publisherChannelLocked returns the shared publisher channel, recreating it
// if needed. Caller must hold pubMu.
func (s *RabbitMQService) publisherChannelLocked(ctx context.Context) (*amqp.Channel, error) {
	if s.pubCh != nil && !s.pubCh.IsClosed() {
		return s.pubCh, nil
	}
	conn, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if s.config.PublisherConfirm {
		if err := ch.Confirm(false); err != nil {
			ch.Close()
			return nil, fmt.Errorf("enabling publisher confirms: %w", err)
		}
	}
	s.pubCh = ch
	return ch, nil
}

func (s *RabbitMQService) resetPublisherChannel() {
	s.pubMu.Lock()
	if s.pubCh != nil {
		s.pubCh.Close()
		s.pubCh = nil
	}
	s.pubMu.Unlock()
}

// Subscribe registers a handler for a queue. Call Start() after registering
// all handlers. The handler must be func(*amqp.Delivery, T) error where T is
// interface{} or a concrete type to JSON-unmarshal into.
func (s *RabbitMQService) Subscribe(queue string, handler interface{}, options ...RabbitSubscribeOption) {
	opts := &rabbitSubscribeOptions{
		ExchangeKind: "topic",
		Prefetch:     s.config.PrefetchCount,
		Concurrency:  1,
		RetryDelay:   time.Second,
		LockTTL:      15 * time.Second,
	}
	for _, o := range options {
		o(opts)
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	finalHandler, targetType := s.wrapHandler(handler)

	s.subsMu.Lock()
	s.subs = append(s.subs, &rabbitSubscription{
		queue:      queue,
		handler:    finalHandler,
		targetType: targetType,
		opts:       opts,
	})
	s.subsMu.Unlock()

	Logger(context.Background()).
		WithField("queue", s.prefixed(queue)).
		WithField("exchange", opts.Exchange).
		WithField("concurrency", opts.Concurrency).
		WithField("cluster_lock", opts.LockKey).
		WithField("component", "rabbitmq").
		Infof("Registered handler for queue %s", queue)
}

func (s *RabbitMQService) wrapHandler(handler interface{}) (RabbitHandler, reflect.Type) {
	v := reflect.ValueOf(handler)
	t := v.Type()

	if t.Kind() != reflect.Func || t.NumIn() != 2 || t.NumOut() != 1 {
		panic(fmt.Sprintf("rabbitmq subscribe: handler must be a function with 2 inputs and 1 output, got %T", handler))
	}

	arg0 := t.In(0)
	if arg0 != reflect.TypeOf(&amqp.Delivery{}) {
		panic(fmt.Sprintf("rabbitmq subscribe: first argument must be *amqp.Delivery, got %v", arg0))
	}

	arg1 := t.In(1)
	if arg1.Kind() == reflect.Interface && arg1.NumMethod() == 0 {
		return func(d *amqp.Delivery, data interface{}) error {
			res := v.Call([]reflect.Value{reflect.ValueOf(d), reflect.ValueOf(data)})
			if res[0].IsNil() {
				return nil
			}
			return res[0].Interface().(error)
		}, nil
	}

	return func(d *amqp.Delivery, data interface{}) error {
		if data == nil {
			if arg1.Kind() == reflect.Ptr {
				res := v.Call([]reflect.Value{reflect.ValueOf(d), reflect.Zero(arg1)})
				if res[0].IsNil() {
					return nil
				}
				return res[0].Interface().(error)
			}
			return fmt.Errorf("rabbitmq handler: received nil data for non-pointer type %v", arg1)
		}

		vData := reflect.ValueOf(data)
		if !vData.Type().AssignableTo(arg1) {
			return fmt.Errorf("rabbitmq handler: data type %T is not assignable to %v (unmarshal might have failed)", data, arg1)
		}

		res := v.Call([]reflect.Value{reflect.ValueOf(d), vData})
		if res[0].IsNil() {
			return nil
		}
		return res[0].Interface().(error)
	}, arg1
}

// Start launches a supervisor goroutine per subscription. Should be called
// after all Subscribe() calls.
func (s *RabbitMQService) Start() error {
	s.startedMu.Lock()
	if s.started {
		s.startedMu.Unlock()
		return fmt.Errorf("rabbitmq consumers already started")
	}
	s.started = true
	s.startedMu.Unlock()

	s.subsMu.Lock()
	subs := append([]*rabbitSubscription{}, s.subs...)
	s.subsMu.Unlock()

	if len(subs) == 0 {
		return fmt.Errorf("no queues registered, call Subscribe() first")
	}

	for _, sub := range subs {
		s.wg.Add(1)
		go func(sub *rabbitSubscription) {
			defer s.wg.Done()
			if sub.opts.LockKey != "" {
				s.runWithClusterLock(sub)
			} else {
				s.consumeForever(s.ctx, sub)
			}
		}(sub)
	}
	return nil
}

// runWithClusterLock is the leader-election loop: consume only while holding
// the redsync lock; on loss, stop consuming, drain in-flight work, and rejoin
// the election. Mirrors StartCronLeaderElection.
func (s *RabbitMQService) runWithClusterLock(sub *rabbitSubscription) {
	rs := new(Redis).RedSync(sub.opts.LockConnection)
	if rs == nil {
		Logger(context.Background()).
			WithField("queue", sub.queue).
			WithField("component", "rabbitmq").
			Error("RedSync not initialized, cannot start cluster-locked consumer")
		return
	}

	lockTTL := sub.opts.LockTTL
	logger := Logger(context.Background()).
		WithField("queue", s.prefixed(sub.queue)).
		WithField("lock_key", sub.opts.LockKey).
		WithField("component", "rabbitmq")

	for {
		if s.ctx.Err() != nil {
			return
		}

		mutex := rs.NewMutex(sub.opts.LockKey, redsync.WithExpiry(lockTTL), redsync.WithTries(1))
		if err := mutex.Lock(); err != nil {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(lockTTL / 3):
			}
			continue
		}

		logger.Info("Acquired cluster lock, starting consumer")

		consumerCtx, cancelConsumer := context.WithCancel(s.ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.consumeForever(consumerCtx, sub)
		}()

		lostLock := false
		ticker := time.NewTicker(lockTTL / 3)
	RefreshLoop:
		for {
			select {
			case <-s.ctx.Done():
				break RefreshLoop
			case <-ticker.C:
				if ok, err := mutex.Extend(); !ok || err != nil {
					logger.WithError(err).Error("Failed to extend cluster lock, stopping consumer")
					lostLock = true
					break RefreshLoop
				}
			}
		}
		ticker.Stop()

		cancelConsumer()
		<-done // consumeOnce waits for its workers, so in-flight messages are drained
		logger.Info("Cluster-locked consumer stopped")

		if !lostLock {
			if _, err := mutex.Unlock(); err != nil {
				logger.WithError(err).Warn("Failed to release cluster lock")
			}
		}

		if s.ctx.Err() != nil {
			return
		}
		time.Sleep(time.Second)
	}
}

// consumeForever keeps a consumer alive across connection/channel loss.
func (s *RabbitMQService) consumeForever(ctx context.Context, sub *rabbitSubscription) {
	for ctx.Err() == nil {
		if err := s.consumeOnce(ctx, sub); err != nil && ctx.Err() == nil {
			Logger(context.Background()).
				WithError(err).
				WithField("queue", s.prefixed(sub.queue)).
				WithField("component", "rabbitmq").
				Error("Consumer error, restarting")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.config.ReconnectDelay):
		}
	}
}

// consumeOnce declares topology, consumes until the channel dies or ctx is
// cancelled, and waits for its workers before returning.
func (s *RabbitMQService) consumeOnce(ctx context.Context, sub *rabbitSubscription) error {
	conn, err := s.connection(ctx)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	queueName, err := s.declareTopology(ch, sub)
	if err != nil {
		return fmt.Errorf("declaring topology: %w", err)
	}

	if err := ch.Qos(sub.opts.Prefetch, 0, false); err != nil {
		return fmt.Errorf("setting qos: %w", err)
	}

	deliveries, err := ch.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("starting consume: %w", err)
	}

	Logger(context.Background()).
		WithField("queue", queueName).
		WithField("prefetch", sub.opts.Prefetch).
		WithField("concurrency", sub.opts.Concurrency).
		WithField("component", "rabbitmq").
		Info("Consumer started")

	// Closing the channel on ctx cancellation ends the deliveries range below.
	consumeDone := make(chan struct{})
	defer close(consumeDone)
	go func() {
		select {
		case <-ctx.Done():
			ch.Close()
		case <-consumeDone:
		}
	}()

	var workers sync.WaitGroup
	for i := 0; i < sub.opts.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for d := range deliveries {
				s.handleDelivery(sub, queueName, d)
			}
		}()
	}
	workers.Wait()
	return nil
}

func (s *RabbitMQService) declareTopology(ch *amqp.Channel, sub *rabbitSubscription) (string, error) {
	queueName := s.prefixed(sub.queue)

	if sub.opts.Passive {
		// External broker: the queue already exists and we may not have
		// configure permission — consume it as-is.
		return queueName, nil
	}

	args := amqp.Table{}
	if sub.opts.Quorum {
		args["x-queue-type"] = "quorum"
	}
	if sub.opts.SingleActiveConsumer {
		args["x-single-active-consumer"] = true
	}
	if sub.opts.DeadLetter {
		dlxName := queueName + ".dlx"
		dlqName := queueName + ".dlq"
		if err := ch.ExchangeDeclare(dlxName, "fanout", true, false, false, false, nil); err != nil {
			return "", err
		}
		if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
			return "", err
		}
		if err := ch.QueueBind(dlqName, "", dlxName, false, nil); err != nil {
			return "", err
		}
		args["x-dead-letter-exchange"] = dlxName
	}
	for k, v := range sub.opts.QueueArgs {
		args[k] = v
	}

	if _, err := ch.QueueDeclare(queueName, true, sub.opts.AutoDelete, false, false, args); err != nil {
		return "", err
	}

	if sub.opts.Exchange != "" {
		exchangeName := s.prefixed(sub.opts.Exchange)
		if err := ch.ExchangeDeclare(exchangeName, sub.opts.ExchangeKind, true, false, false, false, nil); err != nil {
			return "", err
		}
		routingKeys := sub.opts.RoutingKeys
		if len(routingKeys) == 0 {
			routingKeys = []string{"#"}
		}
		for _, rk := range routingKeys {
			if err := ch.QueueBind(queueName, rk, exchangeName, false, nil); err != nil {
				return "", err
			}
		}
	}

	return queueName, nil
}

func (s *RabbitMQService) handleDelivery(sub *rabbitSubscription, queueName string, d amqp.Delivery) {
	s.messageWg.Add(1)
	defer s.messageWg.Done()
	defer func() {
		if r := recover(); r != nil {
			Logger(context.Background()).
				WithField("queue", queueName).
				WithField("panic", r).
				WithField("component", "rabbitmq").
				Error("Panic in message handler")
			d.Nack(false, false)
		}
	}()

	var dedupKey string
	if sub.opts.DedupTTL > 0 {
		dedupKey = s.dedupKey(queueName, &d)
		acquired, err := s.tryDedup(dedupKey, sub.opts.DedupTTL, sub.opts.DedupConnection)
		if err != nil {
			// Fail open: broken Redis must not stop consumption.
			Logger(context.Background()).WithError(err).WithField("queue", queueName).WithField("component", "rabbitmq").Warn("Dedup check failed, processing anyway")
		} else if !acquired {
			Logger(context.Background()).WithField("queue", queueName).WithField("message_id", d.MessageId).WithField("component", "rabbitmq").Debug("Duplicate message skipped")
			d.Ack(false)
			return
		}
	}

	data := decodeRabbitPayload(sub.targetType, d.Body)

	var err error
	maxAttempts := sub.opts.MaxRetry + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = sub.handler(&d, data); err == nil {
			d.Ack(false)
			return
		}
		if attempt < maxAttempts {
			Logger(context.Background()).
				WithField("queue", queueName).
				WithField("attempt", attempt).
				WithField("max_retry", sub.opts.MaxRetry).
				WithError(err).
				WithField("component", "rabbitmq").
				Warn("Handler failed, retrying...")
			time.Sleep(sub.opts.RetryDelay)
		}
	}

	Logger(context.Background()).
		WithField("queue", queueName).
		WithField("attempts", maxAttempts).
		WithError(err).
		WithField("component", "rabbitmq").
		Error("Handler failed after all retry attempts")

	// Release the dedup claim so a redelivery can be processed by someone.
	if dedupKey != "" {
		s.releaseDedup(dedupKey, sub.opts.DedupConnection)
	}
	// requeue=false dead-letters the message when WithDeadLetter is set,
	// otherwise the broker drops it.
	d.Nack(false, sub.opts.RequeueOnFailure)
}

func (s *RabbitMQService) dedupKey(queueName string, d *amqp.Delivery) string {
	id := d.MessageId
	if id == "" {
		sum := sha256.Sum256(d.Body)
		id = hex.EncodeToString(sum[:16])
	}
	return "rabbitmq:dedup:" + queueName + ":" + id
}

func (s *RabbitMQService) tryDedup(key string, ttl time.Duration, connectionName string) (bool, error) {
	db := new(Redis).DB(connectionName)
	if db == nil {
		return false, fmt.Errorf("redis connection %q not initialized", connectionName)
	}
	return db.SetNX(context.Background(), key, "1", ttl).Result()
}

func (s *RabbitMQService) releaseDedup(key string, connectionName string) {
	db := new(Redis).DB(connectionName)
	if db == nil {
		return
	}
	if err := db.Del(context.Background(), key).Err(); err != nil {
		Logger(context.Background()).WithError(err).WithField("key", key).WithField("component", "rabbitmq").Warn("Failed to release dedup key")
	}
}

func decodeRabbitPayload(targetType reflect.Type, body []byte) interface{} {
	if targetType == nil {
		var data interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			return body
		}
		return data
	}

	isPtr := targetType.Kind() == reflect.Ptr
	var val reflect.Value
	if isPtr {
		val = reflect.New(targetType.Elem())
	} else {
		val = reflect.New(targetType)
	}

	if err := json.Unmarshal(body, val.Interface()); err != nil {
		switch targetType.Kind() {
		case reflect.String:
			return string(body)
		default:
			return body
		}
	}
	if isPtr {
		return val.Interface()
	}
	return val.Elem().Interface()
}

// Stop gracefully stops all consumers and waits for in-flight messages.
func (s *RabbitMQService) Stop() {
	s.startedMu.Lock()
	if !s.started {
		s.startedMu.Unlock()
		return
	}
	s.started = false
	s.startedMu.Unlock()

	Logger(context.Background()).WithField("component", "rabbitmq").Info("Stopping consumers")
	s.cancel()
	s.wg.Wait()
	s.messageWg.Wait()
	Logger(context.Background()).WithField("component", "rabbitmq").Info("Consumers stopped, all in-flight messages processed")
}

// Close stops consumers and closes the publisher channel and connection.
func (s *RabbitMQService) Close() error {
	s.Stop()
	s.cancel()

	s.pubMu.Lock()
	if s.pubCh != nil {
		s.pubCh.Close()
		s.pubCh = nil
	}
	s.pubMu.Unlock()

	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn != nil && !s.conn.IsClosed() {
		if err := s.conn.Close(); err != nil {
			return fmt.Errorf("rabbitmq close: %w", err)
		}
	}
	Logger(context.Background()).WithField("component", "rabbitmq").Info("Connection closed")
	return nil
}
