# RabbitMQ — Concepts, การใช้งาน bootstrap, และ design pattern

เอกสารภาษาไทยประกอบ `bootstrap/rabbitmq.go` (ดู API reference เต็มใน [README.md](../README.md#rabbitmq-integration))

---

## 1. Concept พื้นฐาน: Exchange / Routing Key / Queue

RabbitMQ **ไม่มี "topic" เป็น object จริง ๆ** (ต่างจาก Kafka) — มี 3 อย่างนี้เท่านั้น:

| ส่วน | หน้าที่ | เทียบภาษาคน |
|---|---|---|
| **Exchange** | ตัวกระจาย message (ไม่เก็บอะไร) | ไปรษณีย์กลาง |
| **Routing key** | ป้ายที่ติดบน message | ที่อยู่บนซอง |
| **Queue** | ที่เก็บ message จริง consumer มาอ่านจากตรงนี้ | ตู้จดหมาย |

ที่เรียกกันว่า "topic" คือ exchange ชนิด `topic` + routing key เช่น `bet.settled`

### การอ่าน code

```go
bootstrap.Rabbit.Subscribe(
    "bet-settlements",        // ← QUEUE (ชื่อกล่อง — ตัวตัดสินเรื่อง scale)
    handler,
    bootstrap.BindExchange(
        "game-events",        // ← EXCHANGE
        "topic",              // ← ชนิด exchange (topic = route ตาม pattern ได้ เช่น bet.*)
        "bet.settled",        // ← ROUTING KEY ที่สนใจ
    ),
)

bootstrap.Rabbit.Publish(ctx,
    "bet.settled",                          // ← ROUTING KEY (ไม่ใช่ชื่อ queue!)
    data,
    bootstrap.WithExchange("game-events"),  // ← EXCHANGE
)

// แบบย่อ: ไม่ใส่ WithExchange = arg แรกกลายเป็นชื่อ QUEUE ส่งตรงเข้า queue เลย
bootstrap.Rabbit.Publish(ctx, "bet-settlements", data)
```

ฝั่ง publisher ไม่รู้จัก queue — โยนเข้า exchange พร้อมป้าย แล้ว exchange ดูว่าป้ายนี้มี queue ไหนมา bind รอไว้

---

## 2. Scale หลายเครื่อง: message เข้าเครื่องเดียวหรือทุกเครื่อง?

**ตัวตัดสินคือชื่อ queue ไม่ใช่ exchange:**

```
แบบ work queue (แบ่งงานกัน):                 แบบ broadcast (ได้ทุกเครื่อง):

exchange ──▶ queue "work-a" ──▶ เครื่อง 1 ✅    exchange ──▶ queue "a.node1" ──▶ เครื่อง 1 ✅
                          └──▶ เครื่อง 2        └────────▶ queue "a.node2" ──▶ เครื่อง 2 ✅
```

- **Queue ชื่อเดียวกันทุกเครื่อง** (รันโค้ดเดียวกัน scale ออก) → broker แจก round-robin, message แต่ละตัวเข้า**เครื่องเดียว** — งานไม่ซ้ำ scale worker ได้เลย ไม่ต้องมี lock
- **แต่ละเครื่องมี queue ของตัวเอง** bind exchange เดียวกัน → ทุกเครื่องได้ **copy ครบ** (เช่น cache invalidation):

```go
queueName := "cache-invalidate." + os.Getenv("HOSTNAME")
bootstrap.Rabbit.Subscribe(queueName, handler,
    bootstrap.BindExchange("cache-events", "fanout"),
    bootstrap.WithAutoDelete(), // queue หายเมื่อเครื่องดับ
)
```

**Lock ราย message มีในตัวอยู่แล้ว:** message ที่ยังไม่ ack จะไม่ถูกส่งให้ consumer อื่น ถ้าเครื่องตายกลางทาง broker จะ redeliver (นี่คือที่มาของความจำเป็นเรื่อง dedup — ดูข้อ 3)

---

## 3. Multi-cluster options (เมื่อ competing consumers ไม่พอ)

| Option | Scope | ใช้เมื่อ |
|---|---|---|
| `WithSingleActiveConsumer()` | RabbitMQ cluster เดียว | ต้องการลำดับเป๊ะ / ทำงานทีละ instance — broker เลือก active ตัวเดียว failover ทันที |
| `WithClusterLock(key)` | หลาย k8s cluster ที่แชร์ Redis | ให้ consume แค่ cluster เดียว (active/standby ทั้ง site) — redsync leader election |
| `WithDedup(ttl)` | ราย message | กัน redelivery ซ้ำ (เครื่องตายหลังทำงานเสร็จแต่ก่อน ack) — Redis SETNX ตาม MessageId |

งานทั่วไปไม่ต้องใช้อะไรเลย ปล่อย broker แจกเอง — หยิบ lock เฉพาะงานห้ามรันซ้อน (sync/settlement job) และใส่ `WithDedup` กับงานเงินเสมอ

---

## 4. Design: ต่อ feed ภายนอก (LSports) ใช้กับหลาย environment

### ปัญหา

LSports ส่ง feed ผ่าน RabbitMQ **ของเขา** — 1 subscription = 1 queue ถ้าเอา staging กับ production ไปเกาะ queue นั้นพร้อมกัน = competing consumers → **message ถูกแบ่งครึ่ง ไม่ใช่ copy** ทั้งสอง env ได้ข้อมูลไม่ครบทั้งคู่โดยไม่มี error ใด ๆ

### สถาปัตยกรรมที่แนะนำ: Feed Relay + Fan-out

```
        LSports RabbitMQ (broker ของเขา)
                 │
                 │ consumer ตัวเดียวเท่านั้น
                 │ (WithClusterLock กันรันซ้อน)
                 ▼
        ┌─────────────────┐
        │  feed-relay      │  หน้าที่เดียว: รับ → ติด MessageId → publish ต่อ
        │  (active/standby)│  ack ต้นทางหลัง publisher confirm เท่านั้น
        └────────┬────────┘
                 │ publish → exchange "lsports.feed" (topic)
                 │ routing key ตามชนิดข้อมูล เช่น odds.live, fixture.update
                 ▼
        RabbitMQ ของเราเอง (production broker)
           ├── queue lsports.production   ← prod consumers (scale ได้ตามปกติ)
           └── (Shovel plugin) ──▶ staging broker ──▶ queue lsports.staging
```

### เหตุผลของแต่ละชิ้น

**Relay ตัวเดียว, active/standby ด้วย `WithClusterLock`** — ต่อ LSports ได้ connection เดียว ถ้ารัน relay หลายตัวโดยไม่ lock ข้อมูลจะโดนแบ่ง ใช้:

```go
relay.Subscribe("lsports-inbound", relayHandler,
    bootstrap.WithClusterLock("rabbitmq:lock:lsports-relay"),
    bootstrap.WithClusterLockTTL(15*time.Second),
)
```

ตัวสำรอง standby อยู่ พอตัวหลักตาย takeover ภายใน ~15s

**Ack หลัง confirm** — relay ต้องเปิด `PublisherConfirm: true` และ ack message ฝั่ง LSports **หลังจาก** broker ของเรา confirm แล้วเท่านั้น → ไม่มี message หายระหว่างส่งต่อ (แลกกับโอกาสซ้ำเล็กน้อย → แก้ด้วย dedup ปลายทาง)

**แยก env ด้วย Shovel ไม่ใช่ dual-publish ในโค้ด** — ถ้า relay publish เข้า 2 broker เอง เวลา staging broker ล่ม relay จะค้างหรือต้องเลือก drop เอง ใช้ [RabbitMQ Shovel plugin](https://www.rabbitmq.com/shovel.html) copy จาก exchange ฝั่ง prod → staging broker แทน: prod ไม่รู้จัก staging เลย, staging ล่มก็ไม่กระทบ, Shovel retry ให้เอง

ถ้าใช้ **broker เดียวแชร์กัน** (ประหยัดกว่า เหมาะช่วงแรก): bind 2 queue จาก exchange เดียวกัน แล้ว**ต้องจำกัด queue ฝั่ง staging**:

```go
// staging consumer — กัน staging ดับนาน ๆ แล้ว message ท่วม broker จนกระทบ prod
bootstrap.Rabbit.Subscribe("lsports.staging", handler,
    bootstrap.BindExchange("lsports.feed", "topic", "#"),
    bootstrap.WithQueueArgs(amqp.Table{
        "x-max-length":  100000,          // เก่าสุดถูกทิ้งเมื่อเกิน
        "x-message-ttl": int32(3600000),  // 1 ชม.
    }),
)
```

(ฝั่ง production ไม่ใส่ limit — และใช้ `Prefix` ใน config แยก namespace ต่อ env ได้: `prod_` / `stg_`)

**Dedup ปลายทาง** — consumer ทั้งสอง env ใส่ `WithDedup` โดย relay ติด `WithMessageID` จาก message id ของ LSports:

```go
// ใน relay
ourRabbit.Publish(ctx, routingKey, body,
    bootstrap.WithExchange("lsports.feed"),
    bootstrap.WithMessageID(lsportsMsgID))

// ใน consumer
bootstrap.Rabbit.Subscribe("lsports.production", handler,
    bootstrap.BindExchange("lsports.feed", "topic", "odds.#"),
    bootstrap.WithDedup(time.Hour))
```

**เก็บ raw feed ไว้ replay** — ให้ relay (หรือ consumer แยก queue หนึ่ง) เขียน raw message ลง ClickHouse/S3 ด้วย ได้ 2 อย่าง: audit ย้อนหลัง และ staging สามารถ replay ข้อมูลจริงมาเทสได้โดยไม่ต้องรอ feed สด

**Gap recovery** — feed แบบ odds มีลำดับ/sequence ถ้า relay ดับช่วง failover อาจมีรูข้อมูล ให้ relay เช็ค sequence แล้วเรียก snapshot/recovery API ของ LSports resync เมื่อเจอ gap (อย่าพึ่ง queue อย่างเดียว)

### สรุป checklist

- [ ] consumer ต่อ LSports มีตัวเดียว (`WithClusterLock`) — ห้ามให้ env ต่อตรง
- [ ] relay ack หลัง publisher confirm, ติด MessageId เสมอ
- [ ] แยก env: Shovel ข้าม broker (แนะนำ) หรือ 2 queue + limit ฝั่ง staging
- [ ] consumer ทุก env ใช้ `WithDedup`
- [ ] เก็บ raw feed สำหรับ replay/audit
- [ ] มี sequence-gap detection + resync จาก snapshot API
