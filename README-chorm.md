# chorm (ClickHouse ORM-lite)

A lightweight, ClickHouse-friendly GORM-like layer built on:

- `github.com/ClickHouse/clickhouse-go/v2` (Native)
- `github.com/doug-martin/goqu/v9` (SQL builder)

## What it is good for

- Fast **batch inserts** (PrepareBatch)
- Flexible **reporting queries** (GROUP BY, argMaxMerge, etc.)
- GORM-like ergonomics: `WhereRaw`, `SelectRawMulti`, `Scopes`, `Count`, `Exists`, `Pluck`
- GORM-like **Preload** (IN + map) for `HasMany` and `HasOne`

## What it is NOT

- OLTP transactions
- Row-by-row UPDATE patterns
- Full ORM with relations/migrations

ClickHouse works best with **insert-only** patterns + MV + snapshot tables.

---

## Install

```bash
go get github.com/ClickHouse/clickhouse-go/v2
go get github.com/doug-martin/goqu/v9
```

Drop `chorm.go` into your project (e.g. `internal/chorm`).

---

## Connect (Native + TLS :9440)

```go
tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
opts := chorm.RecommendOptions("host:9440", "cgb2c", "user", "pass", tlsCfg)

conn, err := clickhouse.Open(opts)
if err != nil { panic(err) }

repo := chorm.New(conn).WithDebug(log.Default())
ctx := context.Background()
_ = conn.Ping(ctx)
```

> IMPORTANT: `Addr` must be `host:port` only (no `/db` path).
> Set DB via `Auth.Database`.

---

## Define a model (db tags + TableName)

```go
type BetEvent struct {
    EventTime  time.Time `db:"event_time"`
    OperatorID uint32    `db:"operator_id"`
    UserID     uint64    `db:"user_id"`
    GameKey    string    `db:"game_key"`
    BetAmount  float64   `db:"bet_amount"`
    NetAmount  float64   `db:"net_amount"`
    Status     string    `db:"status"`
}
func (BetEvent) TableName() string { return "bet_events" }
```

### db tag options: `-`, `,json`, `,map`, and `,select`

- **`db:"-"`** — **Ignore the field** (skip for both SELECT and INSERT), like GORM. Use for in-memory or derived fields that are not stored in ClickHouse.
- **`,json`** — For ClickHouse **String** columns that store JSON. The ORM `json.Marshal`s on insert and `json.Unmarshal`s on scan.
- **`,map`** — For ClickHouse **Map** / **Array** columns. The ORM passes the Go map/slice to the driver as-is so it can serialize in the format ClickHouse expects (do not use a JSON string here).
- **`,select`** or **`,select_only`** — The field is included in **SELECT** (and in scan when the query returns that column) but **excluded from INSERT**. Use for computed columns, view-only columns, or any column you do not want to write when using `InsertOne` / `InsertBatch` / `InsertBatchChunked`.

```go
type GameEvent struct {
    ID       uint64            `db:"id"`
    Name     string            `db:"name"`
    Metadata map[string]any    `db:"metadata,json"`   // CH String → JSON string
    Tags     []string          `db:"tags,json"`      // CH String → JSON string
    Config   *ServerConfig     `db:"config,json"`     // CH String → JSON string
}
func (GameEvent) TableName() string { return "game_events" }
```

Example with Map/Array columns (use `,map`) and a select-only column (use `,select`):

```go
type Bet struct {
    ID           uint64              `db:"id"`
    Transactions map[string][]string  `db:"transactions,map"`  // CH Map(K, Array(V))
    Share        map[string]float64  `db:"share,map"`         // CH Map
    GameDetail   map[string]string   `db:"game_detail,map"`    // CH Map(String,String)
    RawBet       interface{}         `db:"raw_bet,json"`      // CH String → JSON
    SumAmount    float64             `db:"sum_amount,select"` // SELECT only; not inserted
}
```

ClickHouse DDL: use **String** for `,json` and **Map/Array** for `,map`:

```sql
CREATE TABLE game_events (
    id       UInt64,
    name     String,
    metadata String,  -- stored as JSON string
    tags     String,
    config   String
) ENGINE = MergeTree() ORDER BY id;
```

Insert and query work exactly the same -- no extra steps:

```go
// Insert -- Metadata & Tags are auto-marshalled to JSON strings
events := []GameEvent{
    {ID: 1, Name: "spin", Metadata: map[string]any{"rtp": 96.5}, Tags: []string{"slot", "vip"}},
    {ID: 2, Name: "deal", Metadata: map[string]any{"hands": 2},  Tags: []string{"card"}},
}
chorm.Must(repo.InsertBatch(ctx, &GameEvent{}, events))

// Query -- Metadata & Tags are auto-unmarshalled back
var results []GameEvent
_ = repo.Model(&GameEvent{}).
    WhereRaw("name = ?", "spin").
    Find(ctx, &results)

fmt.Println(results[0].Metadata["rtp"]) // 96.5
fmt.Println(results[0].Tags)            // [slot vip]
```

---

## Create table (DDL)

```go
chorm.Must(repo.Exec(ctx, `
CREATE TABLE IF NOT EXISTS bet_events
(
  event_time DateTime,
  operator_id UInt32,
  user_id UInt64,
  game_key String,
  bet_amount Float64,
  net_amount Float64,
  status String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (operator_id, user_id, event_time)
`))
```

---

## Query (GORM-like)

### WHERE raw with placeholders

```go
var rows []BetEvent
err := repo.Model(&BetEvent{}).
    WhereRaw("operator_id = ? AND status = ?", 1, "settled").
    WhereRaw("event_time >= now() - INTERVAL 7 DAY").
    OrderBy("event_time DESC").
    Limit(50).
    Find(ctx, &rows)
```

### SELECT with placeholders (recommended style)

```go
type R struct {
    Val    float64 `db:"val"`
    Blabla uint8   `db:"blabla"`
}

var res []R
err := repo.From("some_table").
    SelectRawMulti(
        chorm.Expr("a + ? AS val", 2),
        chorm.Expr("? AS blabla", 3),
    ).
    WhereRaw("b = ? AND a = ?", 1, 2).
    Find(ctx, &res)
```

### Find one row

```go
var row BetEvent
err := repo.Model(&BetEvent{}).
    WhereRaw("user_id = ?", 999).
    First(ctx, &row)    // LIMIT 1

// or Take (clears ORDER BY, then LIMIT 1)
err = repo.Model(&BetEvent{}).Take(ctx, &row)
```

### Find: slice of values or slice of pointers

`Find` accepts either `*[]T` or `*[]*T`:

```go
var rows []BetEvent
_ = repo.Model(&BetEvent{}).WhereRaw("operator_id = ?", 1).Find(ctx, &rows)

var ptrRows []*BetEvent
_ = repo.Model(&BetEvent{}).WhereRaw("operator_id = ?", 1).Find(ctx, &ptrRows)
```

### Queries that return fewer columns than the struct

When you use `SelectCols(...)` or a custom SELECT that returns fewer columns than the full struct, the ORM uses the **actual columns returned** by the query for scanning. Extra struct fields are left zeroed; result columns that are not in the struct are discarded. You no longer get "expected N destination arguments in Scan, not M".

### Count / Exists / Pluck

```go
q := repo.From("bet_events").WhereRaw("operator_id = ?", 1)

n, _ := q.Count(ctx)
ok, _ := q.Exists(ctx)

var userIDs []uint64
_ = q.Pluck(ctx, goqu.I("user_id"), &userIDs)
```

### Scopes

```go
scopeSettled := func(op uint32) chorm.Scope {
  return func(q *chorm.Query) *chorm.Query {
    return q.WhereRaw("operator_id = ? AND status = ?", op, "settled")
  }
}

var rows []BetEvent
_ = repo.Model(&BetEvent{}).
  Scopes(scopeSettled(1)).
  Limit(10).
  Find(ctx, &rows)
```

### Joins

```go
var res []R

// Typed join (INNER, LEFT, RIGHT, FULL, CROSS, ANY LEFT, ANY RIGHT)
_ = repo.From("orders").
    JoinOn("LEFT", "users", "users.id = orders.user_id").
    WhereRaw("orders.status = ?", "active").
    Find(ctx, &res)

// Convenience helpers
_ = repo.From("orders").
    InnerJoin("users", "users.id = orders.user_id").
    Find(ctx, &res)

_ = repo.From("orders").
    LeftJoinAny("users", "users.id = orders.user_id").
    Find(ctx, &res)

// Raw join (table ON condition)
_ = repo.From("orders").
    JoinRaw("users ON users.id = orders.user_id").
    Find(ctx, &res)
```

---

## Pagination

Use `PagingCH` with a `Query` to get a paginated result set (count + ordered slice + page meta). See [helpers/clickhouse/README.md](helpers/clickhouse/README.md) for full details.

```go
import ClickHouseORM "github.com/hexlinevault/core-api/helpers/clickhouse"

var result []BetEvent
order := ClickHouseORM.GeneratePagingOrderCH(sortBy, sortType)

paginator, err := ClickHouseORM.PagingCH(&ClickHouseORM.PagingConfigCH{
    Query:   repo.Model(&BetEvent{}).WhereRaw("operator_id = ?", 1),
    Page:    page,
    PerPage: 20,
    OrderBy: []*ClickHouseORM.PagingOrderByCH{order},
    Ctx:     ctx,
}, &result)
// paginator.Records, paginator.TotalRecord, paginator.TotalPage, etc.
```

`result` can be `*[]T` or `*[]*T`. Set `All: true` to fetch all rows without LIMIT/OFFSET.

---

## Error handling

`First`, `Take`, and `Find` (with `*Struct` dest) return `chorm.ErrNoRows` when no rows are found:

```go
var row BetEvent
err := repo.Model(&BetEvent{}).
    WhereRaw("user_id = ?", 999).
    First(ctx, &row)

if errors.Is(err, chorm.ErrNoRows) {
    // not found
}
```

---

## Inserts

### Single insert (convenience)

```go
_ = repo.InsertOne(ctx, &BetEvent{}, BetEvent{
  EventTime: time.Now(), OperatorID: 1, UserID: 10001,
  GameKey: "baccarat", BetAmount: 100, NetAmount: -100, Status: "settled",
})
```

### Batch insert (fast)

```go
events := make([]BetEvent, 0, 10000)
// append ...
chorm.Must(repo.InsertBatch(ctx, &BetEvent{}, events))
```

### Chunked insert + retry

```go
chorm.Must(repo.InsertBatchChunked(ctx, &BetEvent{}, events, 20000, 4))
```

### Parallel + dynamic chunking (highest throughput + resilient)

```go
chorm.Must(repo.InsertBatchChunkedParallelDynamic(
  ctx,
  &BetEvent{},
  events,
  20000, // chunk start
  1000,  // min chunk
  6,     // workers
  4,     // retryMax
  30,    // qps (0 = unlimited)
))
```

---

## Preload (GORM-like)

### HasMany preload (IN + map)

```go
type User struct {
  OperatorID uint32     `db:"operator_id"`
  UserID     uint64     `db:"user_id"`
  Name       string     `db:"name"`
  Bets       []BetEvent // populated by PreloadHasMany
}

var users []User
_ = repo.From("user_dim").
  SelectCols("operator_id","user_id","name").
  WhereRaw("operator_id = ?", 1).
  Limit(100).
  Find(ctx, &users)

_ = repo.PreloadHasMany(ctx, &users, chorm.HasManyOpt{
  ChildTable:     "bet_events",
  ChildKeyCol:    "user_id",
  ParentKeyField: "UserID",
  ChildKeyField:  "UserID",
  AttachField:    "Bets",
  Select: func(q *chorm.Query) *chorm.Query {
    return q.SelectCols("operator_id","user_id","event_time","game_key","bet_amount","net_amount","status")
  },
  Where: func(q *chorm.Query) *chorm.Query {
    return q.WhereRaw("operator_id = ? AND status = ?", 1, "settled").
      WhereRaw("event_time >= now() - INTERVAL 7 DAY").
      OrderBy("event_time DESC")
  },
})
```

### HasOne preload (last record, IN + map + Pick)

```go
type UserWithLast struct {
  OperatorID uint32    `db:"operator_id"`
  UserID     uint64    `db:"user_id"`
  LastBet    *BetEvent // populated by PreloadHasOne (must be pointer)
}

var users []UserWithLast
_ = repo.From("user_dim").
  SelectCols("operator_id","user_id").
  WhereRaw("operator_id = ?", 1).
  Limit(100).
  Find(ctx, &users)

_ = repo.PreloadHasOne(ctx, &users, chorm.HasOneOpt{
  ChildTable:     "bet_events",
  ChildKeyCol:    "user_id",
  ParentKeyField: "UserID",
  ChildKeyField:  "UserID",
  AttachField:    "LastBet",
  PickField:      "EventTime",
  PickLatest:     true,
  Select: func(q *chorm.Query) *chorm.Query {
    return q.SelectCols("operator_id","user_id","event_time","game_key","bet_amount","status")
  },
  Where: func(q *chorm.Query) *chorm.Query {
    return q.WhereRaw("operator_id = ? AND status = ?", 1, "settled").
      WhereRaw("event_time >= now() - INTERVAL 7 DAY")
  },
})
```

---

## API quick reference

| Category | Function | Signature |
|---|---|---|
| **Query** | `Model` | `repo.Model(m) *Query` |
| | `From` | `repo.From(table) *Query` |
| **Fetch** | `Find` | `q.Find(ctx, &dest)` -- dest is `*[]T`, `*[]*T`, or `*T` |
| | `First` | `q.First(ctx, &dest)` -- LIMIT 1, dest is `*T` |
| | `Take` | `q.Take(ctx, &dest)` -- no order + LIMIT 1 |
| | `Count` | `q.Count(ctx) (uint64, error)` |
| | `Exists` | `q.Exists(ctx) (bool, error)` |
| | `Pluck` | `q.Pluck(ctx, expr, &slice)` |
| **Insert** | `InsertOne` | `repo.InsertOne(ctx, model, row)` |
| | `InsertBatch` | `repo.InsertBatch(ctx, model, rows)` |
| | `InsertBatchColumns` | `repo.InsertBatchColumns(ctx, model, cols, rows)` |
| | `InsertBatchChunked` | `repo.InsertBatchChunked(ctx, model, rows, chunk, retry)` |
| | `InsertBatchChunkedParallelDynamic` | `repo.InsertBatchChunkedParallelDynamic(ctx, ...)` |
| **Preload** | `PreloadHasMany` | `repo.PreloadHasMany(ctx, &parents, opt)` |
| | `PreloadHasOne` | `repo.PreloadHasOne(ctx, &parents, opt)` |
| **Error** | `ErrNoRows` | `chorm.ErrNoRows` -- returned by `First` / `Take` / `Find(*T)` |
| **Util** | `Must` | `chorm.Must(err)` -- panics on error |
| **Tag** | `db:"col,json"` | String column: `json.Marshal` on insert, `json.Unmarshal` on scan |
| **Tag** | `db:"col,map"`  | Map/Array column: pass Go map/slice to driver (no JSON string) |
| **Tag** | `db:"-"` | Ignore field (skip for SELECT and INSERT), like GORM |
| **Tag** | `db:"col,select"` or `db:"col,select_only"` | Include in SELECT and scan; exclude from INSERT |

> Everything is a method on `*Repo` or `*Query` -- no generics, no package-level functions.
> Pass pointers to slices/structs for results, just like GORM.

---

## Tips for performance

- Prefer **batch inserts** (`PrepareBatch`) over `Exec` per row.
- For dashboards, build **snapshot tables** with `AggregatingMergeTree` + `argMaxState/argMaxMerge`, then query snapshots.
- Keep your `ORDER BY` aligned with query patterns (e.g. `(operator_id, day, user_id, event_time)`).

---

## License

Use freely in your own project; treat as template code.
