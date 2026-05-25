package bootstrap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/hexlinevault/core-api.git/configs"

	"github.com/IBM/sarama"
	"go.elastic.co/apm"
)

var Kafka *KafkaService

// MessageHandler is the callback function type for processing Kafka messages
// Returns error to trigger retry (if MaxRetry > 0)
type MessageHandler func(msg *sarama.ConsumerMessage, data interface{}) error

// handlerConfig holds handler and its retry configuration
type handlerConfig struct {
	Handler    MessageHandler
	TargetType reflect.Type
	MaxRetry   int
	RetryDelay time.Duration
}

type KafkaService struct {
	producer sarama.SyncProducer
	brokers  []string
	config   *configs.KafkaConn
	groupID  string // default group ID

	// Consumer management
	ctx            context.Context
	cancel         context.CancelFunc
	saramaConfig   *sarama.Config
	consumerGroups map[string]sarama.ConsumerGroup
	groupHandlers  map[string]map[string]*handlerConfig // groupID -> topic -> handlerConfig
	handlersMu     sync.RWMutex
	groupTopics    map[string][]string // groupID -> topics
	topicsMu       sync.RWMutex
	wg             sync.WaitGroup
	started        bool
	startedMu      sync.Mutex

	// Zero-downtime rebalance support
	messageWg sync.WaitGroup           // tracks in-flight messages across all partitions
	ready     map[string]chan struct{} // groupID -> ready signal
	readyMu   sync.RWMutex
}

type (
	PublishOption func(*publishOptions)

	publishOptions struct {
		Topic string
		Key   string
	}
)

func WithTopic(topic string) PublishOption {
	return func(o *publishOptions) {
		o.Topic = topic
	}
}

func WithKey(key string) PublishOption {
	return func(o *publishOptions) {
		o.Key = key
	}
}

type (
	SubscribeOption func(*subscribeOptions)

	subscribeOptions struct {
		GroupID    string
		MaxRetry   int
		RetryDelay time.Duration
	}
)

// For override default consumer group
func WithGroupID(groupID string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.GroupID = groupID
	}
}

// WithMaxRetry sets max retry attempts when handler returns error (default 0 = no retry)
func WithMaxRetry(maxRetry int) SubscribeOption {
	return func(o *subscribeOptions) {
		o.MaxRetry = maxRetry
	}
}

// WithRetryDelay sets delay between retry attempts (default 1 second)
func WithRetryDelay(delay time.Duration) SubscribeOption {
	return func(o *subscribeOptions) {
		o.RetryDelay = delay
	}
}

// CreateKafkaService create kafka service with producer and consumer group
func CreateKafkaService(conf *configs.KafkaConn) *KafkaService {
	kafkaConfig := sarama.NewConfig()
	if conf.Version != "" {
		version, err := sarama.ParseKafkaVersion(conf.Version)
		if err == nil {
			kafkaConfig.Version = version
		}
	}
	// Producer settings
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.Retry.Max = conf.ProducerRetry
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForAll

	// Consumer settings
	if conf.Oldest {
		kafkaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	}

	// Rebalance strategy
	switch conf.Assignor {
	case "sticky":
		kafkaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategySticky()}
	case "roundrobin":
		kafkaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	case "range":
		kafkaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	default:
		kafkaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	}

	producer, err := sarama.NewSyncProducer(conf.Brokers, kafkaConfig)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "kafka").Fatal("Error creating kafka producer")
	}

	// Create consumer group (single connection for all topics)
	groupID := conf.ConsumerGroup
	if groupID == "" {
		groupID = "default-group"
	}

	ctx, cancel := context.WithCancel(context.Background())

	Logger(context.Background()).WithField("brokers", conf.Brokers).WithField("group_id", groupID).WithField("component", "kafka").Info("Kafka connected")

	Kafka = &KafkaService{
		producer:       producer,
		brokers:        conf.Brokers,
		config:         conf,
		groupID:        groupID,
		ctx:            ctx,
		cancel:         cancel,
		saramaConfig:   kafkaConfig,
		consumerGroups: make(map[string]sarama.ConsumerGroup),
		groupHandlers:  make(map[string]map[string]*handlerConfig),
		groupTopics:    make(map[string][]string),
		ready:          make(map[string]chan struct{}),
	}

	return Kafka
}

// Publish publish message to kafka
func (s *KafkaService) Publish(ctx context.Context, topic string, data interface{}, options ...PublishOption) error {
	opts := &publishOptions{
		Topic: topic,
	}
	for _, o := range options {
		o(opts)
	}

	if opts.Topic == "" {
		return fmt.Errorf("kafka publish: topic is required")
	}

	if s.config.Prefix != "" {
		opts.Topic = s.config.Prefix + opts.Topic
	}

	var value sarama.Encoder
	switch v := data.(type) {
	case string:
		value = sarama.StringEncoder(v)
	case []byte:
		value = sarama.ByteEncoder(v)
	case sarama.Encoder:
		value = v
	default:
		// Default to JSON
		bytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("kafka publish: failed to marshal data: %w", err)
		}
		value = sarama.ByteEncoder(bytes)
	}

	msg := &sarama.ProducerMessage{
		Topic: opts.Topic,
		Value: value,
	}

	if opts.Key != "" {
		msg.Key = sarama.StringEncoder(opts.Key)
	}

	// Inject APM trace context into Kafka message headers
	if tx := apm.TransactionFromContext(ctx); tx != nil {
		traceContext := tx.TraceContext()
		if traceContext.Trace.Validate() == nil {
			// Format traceparent header (W3C Trace Context format: version-trace-parent-span-flags)
			// Format: 00-{trace_id}-{parent_id}-{flags}
			// trace_id: 32 hex chars (16 bytes), parent_id: 16 hex chars (8 bytes), flags: 2 hex chars (1 byte)
			traceID := hex.EncodeToString(traceContext.Trace[:])
			spanID := hex.EncodeToString(traceContext.Span[:])
			flags := fmt.Sprintf("%02x", traceContext.Options)
			traceparent := fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags)

			msg.Headers = append(msg.Headers, sarama.RecordHeader{
				Key:   []byte("elastic-apm-traceparent"),
				Value: []byte(traceparent),
			})
		}
	}

	_, _, err := s.producer.SendMessage(msg)
	return err
}

// Subscribe registers a handler for a topic. Call Start() after registering all handlers.
func (s *KafkaService) Subscribe(topic string, handler interface{}, options ...SubscribeOption) {
	opts := &subscribeOptions{
		GroupID:    s.groupID, // default
		MaxRetry:   0,         // default no retry
		RetryDelay: time.Second,
	}
	for _, o := range options {
		o(opts)
	}

	if s.config.Prefix != "" {
		topic = s.config.Prefix + topic
	}

	finalHandler, targetType := s.wrapHandler(handler)

	s.handlersMu.Lock()
	if s.groupHandlers[opts.GroupID] == nil {
		s.groupHandlers[opts.GroupID] = make(map[string]*handlerConfig)
	}
	s.groupHandlers[opts.GroupID][topic] = &handlerConfig{
		Handler:    finalHandler,
		TargetType: targetType,
		MaxRetry:   opts.MaxRetry,
		RetryDelay: opts.RetryDelay,
	}
	s.handlersMu.Unlock()

	s.topicsMu.Lock()
	s.groupTopics[opts.GroupID] = append(s.groupTopics[opts.GroupID], topic)
	s.topicsMu.Unlock()

	Logger(context.Background()).
		WithField("topic", topic).
		WithField("group_id", opts.GroupID).
		WithField("max_retry", opts.MaxRetry).
		WithField("retry_delay", opts.RetryDelay).
		WithField("component", "kafka").
		Infof("Registered handler for topic %s", topic)
}

func (s *KafkaService) wrapHandler(handler interface{}) (MessageHandler, reflect.Type) {
	v := reflect.ValueOf(handler)
	t := v.Type()

	if t.Kind() != reflect.Func || t.NumIn() != 2 || t.NumOut() != 1 {
		panic(fmt.Sprintf("kafka subscribe: handler must be a function with 2 inputs and 1 output, got %T", handler))
	}

	arg0 := t.In(0)
	if arg0 != reflect.TypeOf(&sarama.ConsumerMessage{}) {
		panic(fmt.Sprintf("kafka subscribe: first argument must be *sarama.ConsumerMessage, got %v", arg0))
	}

	arg1 := t.In(1)
	// If it's interface{}, we return it as is
	if arg1.Kind() == reflect.Interface && arg1.NumMethod() == 0 {
		return func(msg *sarama.ConsumerMessage, data interface{}) error {
			res := v.Call([]reflect.Value{reflect.ValueOf(msg), reflect.ValueOf(data)})
			if res[0].IsNil() {
				return nil
			}
			return res[0].Interface().(error)
		}, nil
	}

	// It's a specific type
	return func(msg *sarama.ConsumerMessage, data interface{}) error {
		// data is expected to be already of type arg1 or nil if unmarshal failed
		if data == nil {
			// If it's a pointer type, we can pass nil, but if it's a value type we can't
			if arg1.Kind() == reflect.Ptr {
				res := v.Call([]reflect.Value{reflect.ValueOf(msg), reflect.Zero(arg1)})
				if res[0].IsNil() {
					return nil
				}
				return res[0].Interface().(error)
			}
			return fmt.Errorf("kafka handler: received nil data for non-pointer type %v", arg1)
		}

		vData := reflect.ValueOf(data)
		if !vData.Type().AssignableTo(arg1) {
			return fmt.Errorf("kafka handler: data type %T is not assignable to %v (unmarshal might have failed)", data, arg1)
		}

		res := v.Call([]reflect.Value{reflect.ValueOf(msg), vData})
		if res[0].IsNil() {
			return nil
		}
		return res[0].Interface().(error)
	}, arg1
}

// Start begins consuming messages from all registered topics using multiple connections if specified.
// This should be called after all Subscribe() calls.
func (s *KafkaService) Start() error {
	s.startedMu.Lock()
	if s.started {
		s.startedMu.Unlock()
		return fmt.Errorf("kafka consumer already started")
	}
	s.started = true
	s.startedMu.Unlock()

	s.topicsMu.RLock()
	groupTopics := make(map[string][]string)
	for gid, topics := range s.groupTopics {
		groupTopics[gid] = append([]string{}, topics...)
	}
	s.topicsMu.RUnlock()

	if len(groupTopics) == 0 {
		return fmt.Errorf("no topics registered, call Subscribe() first")
	}

	for groupID, topics := range groupTopics {
		if len(topics) == 0 {
			continue
		}

		consumerGroup, err := sarama.NewConsumerGroup(s.brokers, groupID, s.saramaConfig)
		if err != nil {
			return fmt.Errorf("error creating kafka consumer group %s: %w", groupID, err)
		}

		s.consumerGroups[groupID] = consumerGroup

		s.readyMu.Lock()
		s.ready[groupID] = make(chan struct{})
		s.readyMu.Unlock()

		handler := &routingHandler{
			service: s,
			groupID: groupID,
		}

		Logger(context.Background()).WithField("topics", topics).WithField("group_id", groupID).WithField("component", "kafka").Info("Starting consumer")

		s.wg.Add(1)
		go func(gid string, ts []string, h *routingHandler, cg sarama.ConsumerGroup) {
			defer s.wg.Done()
			for {
				if err := cg.Consume(s.ctx, ts, h); err != nil {
					if s.ctx.Err() != nil {
						return
					}
					Logger(context.Background()).WithError(err).WithField("group_id", gid).WithField("component", "kafka").Error("Consumer error, retrying")
					time.Sleep(2 * time.Second)
					continue
				}
				if s.ctx.Err() != nil {
					return
				}
			}
		}(groupID, topics, handler, consumerGroup)

		// Monitor consumer group errors
		go func(gid string, cg sarama.ConsumerGroup) {
			for err := range cg.Errors() {
				Logger(context.Background()).WithError(err).WithField("group_id", gid).WithField("component", "kafka").Error("Consumer group error")
			}
		}(groupID, consumerGroup)
	}

	return nil
}

// Stop gracefully stops the consumer and waits for it to finish processing
func (s *KafkaService) Stop() {
	s.startedMu.Lock()
	if !s.started {
		s.startedMu.Unlock()
		return
	}
	s.started = false // Mark as stopped before releasing lock to prevent re-entry
	s.startedMu.Unlock()

	Logger(context.Background()).WithField("component", "kafka").Info("Stopping consumer")

	// Wait for all in-flight messages to complete first
	s.messageWg.Wait()
	Logger(context.Background()).WithField("component", "kafka").Info("All in-flight messages processed")

	// Then cancel context and wait for goroutines
	s.cancel()
	s.wg.Wait()
	Logger(context.Background()).WithField("component", "kafka").Info("Consumer stopped")
}

// WaitReady blocks until the specified consumer group is ready to receive messages.
// If groupID is empty, it waits for the default group.
func (s *KafkaService) WaitReady(groupID ...string) {
	gid := s.groupID
	if len(groupID) > 0 && groupID[0] != "" {
		gid = groupID[0]
	}

	s.readyMu.RLock()
	ch, ok := s.ready[gid]
	s.readyMu.RUnlock()

	if ok {
		<-ch
	}
}

// Close closes all Kafka connections (producer and consumer)
func (s *KafkaService) Close() error {
	s.Stop()

	var errs []error
	if s.producer != nil {
		if err := s.producer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("producer close: %w", err))
		}
	}

	for gid, cg := range s.consumerGroups {
		if err := cg.Close(); err != nil {
			errs = append(errs, fmt.Errorf("consumer group %s close: %w", gid, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("kafka close errors: %v", errs)
	}
	Logger(context.Background()).WithField("component", "kafka").Info("All connections closed")
	return nil
}

// routingHandler routes messages to the appropriate handler based on topic
type routingHandler struct {
	service *KafkaService
	groupID string
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (h *routingHandler) Setup(session sarama.ConsumerGroupSession) error {
	Logger(context.Background()).
		WithField("group_id", h.groupID).
		WithField("generation_id", session.GenerationID()).
		WithField("claims", session.Claims()).
		WithField("component", "kafka").
		Info("Consumer session started")

	// Signal that consumer is ready (non-blocking, only first time)
	h.service.readyMu.RLock()
	ch, ok := h.service.ready[h.groupID]
	h.service.readyMu.RUnlock()

	if ok {
		select {
		case <-ch:
			// Already closed, recreate for next rebalance
		default:
			close(ch)
		}
	}

	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
// This is called during rebalance - we MUST wait for in-flight messages to complete
func (h *routingHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	Logger(context.Background()).
		WithField("group_id", h.groupID).
		WithField("generation_id", session.GenerationID()).
		WithField("component", "kafka").
		Info("Consumer session ending, waiting for in-flight messages")

	// Wait for all in-flight messages to complete before releasing partitions
	// This ensures zero message loss during rebalance
	h.service.messageWg.Wait()

	Logger(context.Background()).
		WithField("group_id", h.groupID).
		WithField("generation_id", session.GenerationID()).
		WithField("component", "kafka").
		Info("Consumer session ended, all messages processed")

	// Prepare ready channel for next session
	h.service.readyMu.Lock()
	h.service.ready[h.groupID] = make(chan struct{})
	h.service.readyMu.Unlock()

	return nil
}

// ConsumeClaim processes messages for a specific topic partition
// Zero-downtime: Messages are tracked and Cleanup waits for completion
func (h *routingHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-session.Context().Done():
			// Session ended (rebalance or shutdown)
			// Don't wait here - Cleanup will handle waiting
			return nil

		case <-h.service.ctx.Done():
			// Service shutdown requested
			return nil

		case message, ok := <-claim.Messages():
			if !ok {
				// Channel closed
				return nil
			}

			topic := message.Topic

			h.service.handlersMu.RLock()
			groupH, exists := h.service.groupHandlers[h.groupID]
			var hConfig *handlerConfig
			if exists {
				hConfig = groupH[topic]
			}
			h.service.handlersMu.RUnlock()

			if hConfig == nil {
				Logger(context.Background()).WithField("group_id", h.groupID).WithField("topic", topic).WithField("component", "kafka").Warn("No handler for topic, skipping message")
				session.MarkMessage(message, "")
				continue
			}

			var data interface{}
			if hConfig.TargetType != nil {
				// unmarshal into specific type
				var val reflect.Value
				isPtr := hConfig.TargetType.Kind() == reflect.Ptr
				if isPtr {
					val = reflect.New(hConfig.TargetType.Elem())
				} else {
					val = reflect.New(hConfig.TargetType)
				}

				if err := json.Unmarshal(message.Value, val.Interface()); err != nil {
					// Fallback for raw string/bytes if JSON unmarshal fails
					switch hConfig.TargetType.Kind() {
					case reflect.String:
						data = string(message.Value)
					case reflect.Slice:
						if hConfig.TargetType.Elem().Kind() == reflect.Uint8 { // []byte
							data = message.Value
						} else {
							data = message.Value
						}
					default:
						data = message.Value
					}
				} else {
					if isPtr {
						data = val.Interface()
					} else {
						data = val.Elem().Interface()
					}
				}
			} else {
				// unmarshal into interface{}
				if err := json.Unmarshal(message.Value, &data); err != nil {
					data = message.Value
				}
			}

			// Track in-flight message at service level for rebalance safety
			h.service.messageWg.Add(1)
			func(msg *sarama.ConsumerMessage, d interface{}, cfg *handlerConfig) {
				defer h.service.messageWg.Done()
				defer func() {
					if r := recover(); r != nil {
						Logger(context.Background()).WithField("topic", msg.Topic).WithField("panic", r).WithField("component", "kafka").Error("Panic in message handler")
					}
				}()

				// Execute handler with retry logic
				var err error
				maxAttempts := cfg.MaxRetry + 1 // +1 for initial attempt
				for attempt := 1; attempt <= maxAttempts; attempt++ {
					err = cfg.Handler(msg, d)
					if err == nil {
						return // success
					}

					// Log retry attempt
					if attempt < maxAttempts {
						Logger(context.Background()).
							WithField("topic", msg.Topic).
							WithField("attempt", attempt).
							WithField("max_retry", cfg.MaxRetry).
							WithField("retry_delay", cfg.RetryDelay).
							WithError(err).
							WithField("component", "kafka").
							Warn("Handler failed, retrying...")
						time.Sleep(cfg.RetryDelay)
					}
				}

				// All retries exhausted
				if err != nil {
					Logger(context.Background()).
						WithField("topic", msg.Topic).
						WithField("attempts", maxAttempts).
						WithError(err).
						WithField("component", "kafka").
						Error("Handler failed after all retry attempts")
				}
			}(message, data, hConfig)

			session.MarkMessage(message, "")
		}
	}
}
