# github.com/hexlinevault/core-api

## Kafka Integration

This project uses the `github.com/IBM/sarama` library for Kafka integration. The service is initialized in `bootstrap/kafka.go` and is accessible via the global variable `bootstrap.Kafka`.

### Configuration

Ensure the following environment variables are set in your `.env` file or environment:

```env
KAFKA_BROKERS=localhost:9092,localhost:9093
KAFKA_GROUP_ID=my-service-group
KAFKA_VERSION=2.8.0  # Optional, defaults if not set
```

### Usage

#### Publishing Messages

To publish a message to a topic:

```go
import (
    "github.com/hexlinevault/core-api/bootstrap"
)

// Publish a simple string
err := bootstrap.Kafka.Publish(ctx, "my-topic", "hello world")

// Publish with a key (for partitioning)
err := bootstrap.Kafka.Publish(ctx, "my-topic", "user data", bootstrap.WithKey("user-123"))

// Publish a struct (automatically marshaled to JSON)
data := map[string]interface{}{
    "id": 1,
    "name": "test",
}
err := bootstrap.Kafka.Publish(ctx, "my-topic", data)
```

#### Subscribing to Topics

The Kafka service supports multiple consumer groups. By default, it uses the group specified in the initialization, but you can override it for specific topics:

```go
// Register with default consumer group
bootstrap.Kafka.Subscribe("user-created", UserCreatedHandler)

// Register with a specific consumer group
bootstrap.Kafka.Subscribe("notifications", NotificationHandler, bootstrap.WithGroupID("notification-group"))
```

### Production Setup Example

The Kafka service manages separate connections for each consumer group with **zero-downtime rebalance support**:

```go
import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "github.com/IBM/sarama"
    "github.com/hexlinevault/core-api/bootstrap"
)

// 1. Define handler functions (must return error)
func UserCreatedHandler(msg *sarama.ConsumerMessage, data interface{}) error {
    fmt.Printf("User created: %+v\n", data)
    // Return nil on success, or error to trigger retry
    return nil
}

func OrderPlacedHandler(msg *sarama.ConsumerMessage, data interface{}) error {
    fmt.Printf("Order placed: %+v\n", data)
    return nil
}

// 2. Setup in main.go
func main() {
    // ... other initialization ...

    // Create Kafka service (single connection for producer + consumer)
    bootstrap.CreateKafkaService(&configs.KafkaConn{
        Brokers:       strings.Split(os.Getenv("KAFKA_BROKERS"), ","),
        ConsumerGroup: os.Getenv("KAFKA_GROUP_ID"),
        Version:       os.Getenv("KAFKA_VERSION"),
        Assignor:      "sticky", // Recommended for zero-downtime
    })

    // 3. Register all handlers (no connections created yet)
    bootstrap.Kafka.Subscribe("user-created", UserCreatedHandler)
    bootstrap.Kafka.Subscribe("order-placed", OrderPlacedHandler)

    // 4. Start consuming (single connection for all topics)
    if err := bootstrap.Kafka.Start(); err != nil {
        log.Fatal(err)
    }

    // 5. Wait for consumer to be ready (optional)
    bootstrap.Kafka.WaitReady()

    // 6. Graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    <-ctx.Done()
    fmt.Println("Shutting down...")

    // Wait for in-flight messages, then close connections
    bootstrap.Kafka.Close()
}
```

#### Key Features

- **Multiple Group Support**: Manage multiple consumer groups within a single service instance
- **Group Isolation**: Topics can be distributed across different groups for better load balancing
- **Zero-Downtime Rebalance**: In-flight messages complete during rebalance
- **Graceful Shutdown**: Waits for processing before closing
- **Panic Recovery**: Handler panics don't crash the consumer
- **Automatic Retry**: Reconnects on broker failures
- **Handler Retry**: Automatic retry with configurable delay when handler returns error

### Error Handling & Retry

Handlers can return an error to trigger automatic retry. Configure retry behavior per subscription:

```go
import (
    "time"
    "github.com/IBM/sarama"
    "github.com/hexlinevault/core-api/bootstrap"
)

// Handler that may fail
func ProcessPaymentHandler(msg *sarama.ConsumerMessage, data interface{}) error {
    err := processPayment(data)
    if err != nil {
        // Returning error will trigger retry
        return err
    }
    return nil
}

// Subscribe with retry configuration
bootstrap.Kafka.Subscribe(
    "process-payment",
    ProcessPaymentHandler,
    bootstrap.WithMaxRetry(3),                    // Retry up to 3 times (4 attempts total)
    bootstrap.WithRetryDelay(2 * time.Second),   // Wait 2 seconds between retries
)

// Subscribe without retry (default behavior)
bootstrap.Kafka.Subscribe("notifications", NotificationHandler)
// Equivalent to: WithMaxRetry(0), WithRetryDelay(1 * time.Second)
```

#### Retry Behavior

| Option              | Default | Description                                                  |
| ------------------- | ------- | ------------------------------------------------------------ |
| `WithMaxRetry(n)`   | `0`     | Maximum retry attempts after initial failure. `0` = no retry |
| `WithRetryDelay(d)` | `1s`    | Delay between retry attempts                                 |

**Flow:**

1. Handler is called
2. If returns `nil` → success, message is marked as processed
3. If returns `error` → log warning, wait `RetryDelay`, retry
4. After all retries exhausted → log error, message is still marked as processed

**Note:** Messages are always marked as processed after all attempts (success or failure). If you need dead-letter queue (DLQ) support, handle it in your handler after detecting final failure.

### Advanced Configuration

The `KafkaConn` struct in `configs/kafka.go` supports additional settings like:

- `Assignor`: Strategies for consumer group rebalancing (`sticky`, `roundrobin`, `range`).
- `Oldest`: If true, consumes from the oldest offset.
- `ProducerRetry`: Number of retries for the producer.

These can be configured by extending the initialization logic in `main.go`.

## Delayed PubSub

An internal pub/sub system that supports **delays** and prevents **duplicate processing** during horizontal scaling. It uses [github.com/hibiken/asynq](https://github.com/hibiken/asynq) (a Redis-based task queue) as its backend.

Everything lives in `bootstrap/pubsub.go`.

### How it works

```
Publish(ctx, topic, payload, opts...)
       │  opts e.g. asynq.ProcessIn(delay), asynq.Queue(name)
       ▼
asynq Client.EnqueueContext → Redis (queue: default or the one specified)
       │
       ▼
asynq Server (worker) in a goroutine — Start() returns immediately
  → processTask dispatches by task.Type() (topic) to the Subscribed handler
       │  builds a *PubSubMessage and wraps the call in an APM transaction
       ▼
handler(msg *PubSubMessage, data *T)  — core unmarshals the JSON payload into *T
```

- **Immediate:** no option → enqueued on the default queue, runs as soon as a worker is free
- **Delayed:** pass `asynq.ProcessIn(delay)` in the Publish opts
- **Kafka-like:** the subscriber API mirrors the Kafka service — pass a typed handler and the payload is decoded for you; `msg` exposes the topic and per-message context

### Usage

Use a Redis connection that has already been registered (you must call `CreateRedisConnection` first):

```go
// main.go
bootstrap.CreatePubSubService("")        // use the redis connection named "default"
bootstrap.CreatePubSubService("cache")   // use the connection named "cache"

// Register handlers before Start. Handlers can be typed (core unmarshals for you):
//   func(msg *bootstrap.PubSubMessage, data *SendEmail) error
bootstrap.PubSub.Subscribe("send-email", sendEmailHandler)
bootstrap.PubSub.Subscribe("charge-card", chargeCardHandler)

// Start the worker in the background — returns immediately
bootstrap.PubSub.Start()
defer bootstrap.PubSub.Close()   // graceful shutdown, safe to call multiple times

// Publish — payload can be anything (it is marshaled to JSON); opts are asynq.Option
bootstrap.PubSub.Publish(ctx, "send-email", payload)
bootstrap.PubSub.Publish(ctx, "send-email", payload, asynq.ProcessIn(5*time.Second))
bootstrap.PubSub.Publish(ctx, "send-email", payload, asynq.ProcessIn(5*time.Second), asynq.TaskID("unique-id"))
```

### CreatePubSubService / NewPubSubService

| Function | When to use |
|----------|----------|
| `CreatePubSubService(redisConnectionName string)` | In the app — take a registered Redis connection and set the global `bootstrap.PubSub` |
| `NewPubSubService(rdb, queueName string)` | Build a service yourself (e.g. in tests where you want an isolated queue) — if `queueName == ""`, `"default"` is used |

### Handler signatures

`Subscribe(topic string, handler interface{})` accepts three handler shapes (mirroring the Kafka service). Pick whichever fits:

| Shape | Use when |
|-------|----------|
| `func(msg *PubSubMessage, data *T) error` | **typed** — core JSON-unmarshals the payload into a fresh `*T` for you |
| `func(msg *PubSubMessage, payload []byte) error` | raw bytes, but you still want `msg` metadata (topic, context, id) |
| `func(ctx context.Context, payload []byte) error` | legacy raw form (`HandlerFunc`) — ctx only, unmarshal yourself |

`PubSubMessage` carries the per-message data (analogous to `sarama.ConsumerMessage` on Kafka):

```go
type PubSubMessage struct {
    Context context.Context // per-message context, carries the APM transaction
    Topic   string
    Payload []byte           // raw JSON bytes
    ID      string           // asynq task id
}
```

**Typed handler (recommended)** — no manual unmarshal:

```go
type SendEmail struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
}

func sendEmailHandler(msg *bootstrap.PubSubMessage, p *SendEmail) error {
    // p is already decoded; use msg.Context for downstream calls (APM-aware)
    return mailer.Send(msg.Context, p)
}
```

> Use `msg.Context` (not `context.Background()`) so DB/HTTP spans nest under the message's APM transaction.

### Recommended pattern — subscribers package

```go
// subscribers/delayed.go
package subscribers

import (
    "context"
    "encoding/json"

    "github.com/hexlinevault/core-api/bootstrap"
)

const TopicSendEmail = "send-email"

func RegisterDelayed() {
    bootstrap.PubSub.Subscribe(TopicSendEmail, sendEmailHandler)
}

func sendEmailHandler(ctx context.Context, payload []byte) error {
    var p struct {
        To      string `json:"to"`
        Subject string `json:"subject"`
    }
    if err := json.Unmarshal(payload, &p); err != nil {
        return err
    }
    bootstrap.Logger(ctx).Info("send-email: " + p.To)
    return nil
}
```

```go
// main.go
bootstrap.CreateRedisConnection(...)
bootstrap.CreatePubSubService("")

subscribers.RegisterDelayed()

bootstrap.PubSub.Start()
defer bootstrap.PubSub.Close()
```

### Backend (asynq)

- The queue name comes from `CreatePubSubService` (uses `"default"`) or `NewPubSubService(rdb, queueName)`
- Topic = asynq task type; payload = task payload (JSON bytes)
- Publish adds `asynq.Queue(s.queueName)` automatically

### Cluster safety

asynq is designed so that multiple workers can share a Redis queue — each task is claimed and processed by a single worker, so every job is processed **once** even when scaled across many servers (at-least-once delivery).

## System Notification

`utils.TriggerSystemNoti` reports high-level system events (errors, warnings, notifications) to developers. The message is published to a pub/sub topic; a subscriber consumes it and forwards it to Telegram.

The **pub/sub engine is selectable** — either **Kafka** (default) or **Redis** (the Delayed PubSub service above). Both the publisher (`TriggerSystemNoti`) and the subscriber respect the same engine setting, so they stay matched.

Everything lives in `utils/notification.go` and `bootstrap/system-noti.go`.

### How it works

```
TriggerSystemNoti(ctx, type, message, err)
       │  builds a SystemNoti{Environment, Type, Message, Error, Time, CodeLine}
       ▼
publish to topic bootstrap.SYSTEM_NOTI_KAFKA_PUBLISHER
   ├─ engine "kafka" → bootstrap.Kafka.Publish
   └─ engine "redis" → bootstrap.PubSub.Publish
       │
       ▼
subscriber (matching engine)
   ├─ Kafka → utils.SystemNotiSubscriber()
   └─ Redis → utils.SystemNotiRedisSubscriber()
       │
       ▼
format an HTML message → send to Telegram (SYSTEM_NOTI_TELEGRAM_*)
```

### Setup

Call `InitSystemNoti` once at startup. The engine defaults to `"kafka"` for backward compatibility.

```go
// main.go
bootstrap.CreateTelegramBot(os.Getenv("SYSTEM_NOTI_TELEGRAM_BOT_TOKEN"))

bootstrap.InitSystemNoti(&configs.SystemNotiConf{
    TelegramChatId: utils.Pointer(os.Getenv("SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID")),
    // Engine: utils.Pointer("redis"), // "kafka" (default) or "redis"
    // MessageTopic: utils.Pointer("system-notification-publisher"), // override the topic/queue name
})
```

`SystemNotiConf` fields:

| Field | Default | Description |
|-------|---------|-------------|
| `Engine` | `"kafka"` | Pub/sub engine: `"kafka"` or `"redis"` |
| `MessageTopic` | `"system-notification-publisher"` | Topic (Kafka) / task type (Redis) used by both publisher and subscriber |
| `TelegramConnection` | `"default"` | Telegram bot connection name (see `CreateTelegramBot`) |
| `TelegramChatId` | — | Target chat id. **Required** when registering a subscriber |

### Publishing

`TriggerSystemNoti` is engine-agnostic — call it the same way regardless of the configured engine:

```go
import "github.com/hexlinevault/core-api/utils"

utils.TriggerSystemNoti(ctx, utils.NotiTypeError, "failed to settle bet", err)
utils.TriggerSystemNoti(ctx, utils.NotiTypeWarning, "balance check slow", nil)
utils.TriggerSystemNoti(ctx, utils.NotiTypeNotification, "service started", nil)
```

Notification types: `NotiTypeNotification`, `NotiTypeError`, `NotiTypeWarning`. The `CodeLine` (caller file/line) is included for error/warning types.

### Registering the subscriber

Register the subscriber that matches the configured engine. Both require `InitSystemNoti` to have set the Telegram chat id, or they panic at registration time.

```go
// Kafka engine
bootstrap.Kafka.Subscribe(
    bootstrap.SYSTEM_NOTI_KAFKA_PUBLISHER,
    apmmiddlewares.APMKafkaWrapper(utils.SystemNotiSubscriber()),
)

// Redis engine
bootstrap.PubSub.Subscribe(
    bootstrap.SYSTEM_NOTI_KAFKA_PUBLISHER,
    utils.SystemNotiRedisSubscriber(),
)
```

| Subscriber | Engine | Signature |
|------------|--------|-----------|
| `SystemNotiSubscriber()` | Kafka | `func(*sarama.ConsumerMessage, *SystemNoti) error` |
| `SystemNotiRedisSubscriber()` | Redis | `bootstrap.HandlerFunc` (`func(ctx, payload []byte) error`) |

> The Redis engine reuses the Delayed PubSub service, so `bootstrap.CreatePubSubService(...)` must be called before publishing or subscribing with `"redis"`. Likewise the Kafka engine requires `bootstrap.CreateKafkaService(...)`.

## Utils

### Map Helpers (`utils/map.go`)

Convert a slice of struct pointers into a map, specifying which field(s) to use as the key.

#### Usage (ConvertMap)

```go
import "gitlab.com/stand-eleven/api-service.git/utils"

type User struct {
    ID   uint64
    Code string
    Name string
}

func Example() {
    users := []*User{
        {ID: 1, Code: "U001", Name: "Alice"},
        {ID: 2, Code: "U002", Name: "Bob"},
    }

    // 1. Convert using ID as the key (uint64)
    userMapByID := apputils.ConvertMap[uint64, User](users, "ID")
    // Result: map[1:*User{ID:1...}, 2:*User{ID:2...}]

    // 2. Convert using Code as the key (string)
    userMapByCode := apputils.ConvertMap[string, User](users, "Code")
    // Result: map["U001":*User{ID:1...}, "U002":*User{ID:2...}]

    // 3. Convert using multiple fields combined (the result is a string joined with "_")
    userMapCombined := apputils.ConvertMap[string, User](users, "ID", "Code")
    // Result: map["1_U001":*User{ID:1...}, "2_U002":*User{ID:2...}]
}
```

#### Using Pick and Omit (similar to TypeScript)

Select (Pick) or drop (Omit) specific fields from a struct or map. Returns a `map[string]any` (respects JSON tags).

```go
type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

user := User{ID: 1, Name: "User", Email: "user@example.com"}

// 1. Omit: drop the unwanted fields
res := utils.Omit(user, "email", "id")
// Result: map[string]any{"name": "User"}

// 2. Pick: keep only the wanted fields
res := utils.Pick(user, "name")
// Result: map[string]any{"name": "User"}

// 3. Use with slices (OmitSlice / PickSlice)
users := []User{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}
res := utils.OmitSlice(users, "id")
// Result: []map[string]any{ {"name": "A"}, {"name": "B"} }
```

### Pointer & Deep Property Helpers (`utils/pointer.go`, `utils/object.go`)

Safely work with pointers and read values from nested structures (similar to Lodash in Node.js).

#### 1. Reading values from nested data (`NestedValue`)

Read a value from a deeply nested struct, map, or slice using a string path (dot notation). It automatically performs `nil` checks to prevent panics.

```go
type Additional struct { Privilege float64 }
type Transaction struct { Addr *Additional }
type Response struct { Data *Transaction }

func Example() {
    resp := Response{
        Data: &Transaction{
            Addr: &Additional{Privilege: 10.5},
        },
    }

    // Read Privilege by specifying the path
    // If any segment is nil or a field is not found, the fallback (0.0) is returned immediately
    val := apputils.NestedValue(resp, "Data.Addr.Privilege", 0.0) // Result: 10.5

    // Slice/array access by index is supported
    // val := apputils.NestedValue(data, "Items.0.Name", "Unknown")
}
```

#### 2. Basic pointer handling (`Pointer`, `Value`, `ValueOr`)

```go
func ExamplePointer() {
    // 1. Create a pointer from a literal (which Go cannot normally do)
    s := apputils.Pointer("hello") // *string

    // 2. Read a value from a pointer safely (if nil, returns the type's zero value)
    val := apputils.Value(s) // "hello"

    // 3. Read a value from a pointer, or use the default if it is nil
    valWithDefault := apputils.ValueOr(s, "default_val") // "hello"
}
```
