# github.com/hexlinevault/core-api.git

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
    "github.com/hexlinevault/core-api.git/bootstrap"
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
    "github.com/hexlinevault/core-api.git/bootstrap"
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
    "github.com/hexlinevault/core-api.git/bootstrap"
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

ระบบ internal pub/sub ที่รองรับ **delay** และป้องกัน **duplicate processing** ตอน horizontal scale ใช้ [github.com/hibiken/asynq](https://github.com/hibiken/asynq) (Redis-based task queue) เป็น backend

ทุกอย่างอยู่ใน `bootstrap/pubsub.go`

### How it works

```
Publish(ctx, topic, payload, opts...)
       │  opts เช่น asynq.ProcessIn(delay), asynq.Queue(name)
       ▼
asynq Client.EnqueueContext → Redis (queue: default หรือที่กำหนด)
       │
       ▼
asynq Server (worker) ใน goroutine — Start() แล้ว return ทันที
  → processTask กระจายตาม task.Type() (topic) ไปยัง handler ที่ Subscribe ไว้
       │
       ▼
HandlerFunc(ctx, payload []byte)  — payload คือ JSON bytes, unmarshal เองใน handler
```

- **Immediate:** ไม่ส่ง option → ใส่ default queue ทำงานทันทีที่ worker ว่าง
- **Delayed:** ส่ง `asynq.ProcessIn(delay)` ใน opts ของ Publish

### Usage

ใช้ Redis connection ที่ register ไว้แล้ว (ต้องเรียก `CreateRedisConnection` ก่อน):

```go
// main.go
bootstrap.CreatePubSubService("")        // ใช้ redis connection ชื่อ "default"
bootstrap.CreatePubSubService("cache")   // ใช้ connection ชื่อ "cache"

// ลง handlers ก่อน Start (handler รับ ctx กับ payload []byte)
bootstrap.PubSub.Subscribe("send-email", sendEmailHandler)
bootstrap.PubSub.Subscribe("charge-card", chargeCardHandler)

// เริ่ม worker ใน background — return ทันที
bootstrap.PubSub.Start()
defer bootstrap.PubSub.Close()   // graceful shutdown, เรียกซ้ำได้

// Publish — payload เป็นอะไรก็ได้ (จะ marshal เป็น JSON); opts เป็น asynq.Option
bootstrap.PubSub.Publish(ctx, "send-email", payload)
bootstrap.PubSub.Publish(ctx, "send-email", payload, asynq.ProcessIn(5*time.Second))
bootstrap.PubSub.Publish(ctx, "send-email", payload, asynq.ProcessIn(5*time.Second), asynq.TaskID("unique-id"))
```

### CreatePubSubService / NewPubSubService

| ฟังก์ชัน | ใช้เมื่อ |
|----------|----------|
| `CreatePubSubService(redisConnectionName string)` | ใช้ใน app — เอาสาย Redis จากที่ register ไว้ แล้วเซ็ต global `bootstrap.PubSub` |
| `NewPubSubService(rdb, queueName string)` | สร้าง service เอง (เช่น ใน test อยากได้ queue แยก) — ถ้า `queueName == ""` ใช้ `"default"` |

### Handler signature

Handler คือ `bootstrap.HandlerFunc` = `func(ctx context.Context, payload []byte) error`. Payload คือ JSON ที่ Publish marshal มา — ต้อง unmarshal เองใน handler:

```go
func sendEmailHandler(ctx context.Context, payload []byte) error {
    var p struct {
        To      string `json:"to"`
        Subject string `json:"subject"`
    }
    if err := json.Unmarshal(payload, &p); err != nil {
        return err
    }
    return mailer.Send(ctx, p)
}
```

### Recommended pattern — subscribers package

```go
// subscribers/delayed.go
package subscribers

import (
    "context"
    "encoding/json"

    "github.com/hexlinevault/core-api.git/bootstrap"
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

- Queue name มาจาก `CreatePubSubService` (ใช้ `"default"`) หรือ `NewPubSubService(rdb, queueName)`
- Topic = asynq task type; payload = task payload (JSON bytes)
- Publish ใส่ `asynq.Queue(s.queueName)` ให้อัตโนมัติ

### Cluster safety

asynq ออกแบบมาให้หลาย worker ใช้ Redis queue ร่วมกันได้ — แต่ละ task ถูก claim และ process โดย worker เดียว ทำให้แต่ละ job ถูก process **ครั้งเดียว** แม้ scale หลาย server (at-least-once delivery)

## Utils

### Map Helpers (`utils/map.go`)

ใช้สำหรับแปลง Slice ของ Struct Pointer ให้เป็น Map โดยสามารถระบุฟิลด์ที่ต้องการใช้เป็น Key ได้

#### วิธีการใช้งาน (ConvertMap)

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

    // 1. แปลงโดยใช้ ID เป็น Key (uint64)
    userMapByID := apputils.ConvertMap[uint64, User](users, "ID")
    // Result: map[1:*User{ID:1...}, 2:*User{ID:2...}]

    // 2. แปลงโดยใช้ Code เป็น Key (string)
    userMapByCode := apputils.ConvertMap[string, User](users, "Code")
    // Result: map["U001":*User{ID:1...}, "U002":*User{ID:2...}]

    // 3. แปลงโดยใช้หลายฟิลด์ร่วมกัน (ผลลัพธ์จะเป็น string ต่อกันด้วย "_")
    userMapCombined := apputils.ConvertMap[string, User](users, "ID", "Code")
    // Result: map["1_U001":*User{ID:1...}, "2_U002":*User{ID:2...}]
}
```

#### การใช้ Pick และ Omit (คล้าย TypeScript)

ใช้สำหรับเลือก (Pick) หรือตัดออก (Omit) เฉพาะบางฟิลด์จาก Struct หรือ Map โดยจะคืนค่าเป็น `map[string]any` (รองรับ JSON tag)

```go
type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

user := User{ID: 1, Name: "User", Email: "user@example.com"}

// 1. Omit: ตัดฟิลด์ที่ไม่ต้องการออก
res := utils.Omit(user, "email", "id")
// Result: map[string]any{"name": "User"}

// 2. Pick: เลือกเฉพาะฟิลด์ที่ต้องการ
res := utils.Pick(user, "name")
// Result: map[string]any{"name": "User"}

// 3. ใช้งานกับ Slices (OmitSlice / PickSlice)
users := []User{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}
res := utils.OmitSlice(users, "id")
// Result: []map[string]any{ {"name": "A"}, {"name": "B"} }
```

### Pointer & Deep Property Helpers (`utils/pointer.go`, `utils/object.go`)

ใช้สำหรับจัดการ Pointer และดึงข้อมูลจากโครงสร้างที่ซ้อนกัน (Nested Structure) อย่างปลอดภัย (คล้าย Lodash ใน Node.js)

#### 1. การดึงค่าจากข้อมูลที่ซ้อนกัน (`NestedValue`)

ใช้ดึงค่าจาก Struct, Map หรือ Slice ที่ซ้อนกันหลายชั้น โดยระบุเป็น string path (dot notation) ระบบจะจัดการเช็ค `nil` ให้โดยอัตโนมัติเพื่อป้องกันการ Panic

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

    // ดึงค่า Privilege โดยระบุ path
    // หากส่วนใดส่วนหนึ่งเป็น nil หรือหาฟิลด์ไม่เจอ จะคืนค่า fallback (0.0) ทันที
    val := apputils.NestedValue(resp, "Data.Addr.Privilege", 0.0) // Result: 10.5

    // รองรับการเข้าถึง Slice/Array ด้วย index
    // val := apputils.NestedValue(data, "Items.0.Name", "Unknown")
}
```

#### 2. การจัดการ Pointer แบบพื้นฐาน (`Pointer`, `Value`, `ValueOr`)

```go
func ExamplePointer() {
    // 1. สร้าง Pointer จากค่าคงที่ (ปกติ Go ทำไม่ได้)
    s := apputils.Pointer("hello") // *string

    // 2. ดึงค่าจาก Pointer อย่างปลอดภัย (ถ้า nil จะสรุปค่า Zero Value ตาม Type)
    val := apputils.Value(s) // "hello"

    // 3. ดึงค่าจาก Pointer หรือถ้าเป็น nil ให้ใช้ค่า Default
    valWithDefault := apputils.ValueOr(s, "default_val") // "hello"
}
```
