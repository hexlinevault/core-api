# ClickHouse ORM – Pagination

Pagination สำหรับ ClickHouse queries โคลนจาก `helpers.Paging` (GORM) ปรับให้ใช้กับ ClickHouse ORM (`Query`, `Count`, `Find`, `context`).

---

## Types

### PagingConfigCH

Config สำหรับเรียก paging:

| Field     | Type                | Description                                      |
|----------|----------------------|--------------------------------------------------|
| `Query`  | `*Query`             | Query จาก `Model()` หรือ `From()` (required)     |
| `Page`   | `int`                | หน้าที่ต้องการ (default 1)                        |
| `PerPage`| `int`                | จำนวนต่อหน้า (default 10)                        |
| `OrderBy`| `[]*PagingOrderByCH` | เรียงตามคอลัมน์ (optional)                       |
| `ShowSQL`| `bool`               | เปิด debug log SQL (optional)                    |
| `All`    | `bool`               | ถ้า true ไม่ใส่ LIMIT/OFFSET (ดึงทั้งหมด)        |
| `Ctx`    | `context.Context`    | Context สำหรับ Count/Find (nil = Background)     |

### PagingOrderByCH

กำหนดคอลัมน์และทิศทาง sort:

- `SortBy` – ชื่อคอลัมน์ (อนุญาตเฉพาะ `a-zA-Z0-9_.`)
- `SortType` – `"asc"` หรือ `"desc"` (ไม่ระบุถือเป็น ASC)

### PaginatorCH

Response หลัง paging (โครงเดียวกับ `helpers.Paginator`):

| Field         | JSON            | Description        |
|---------------|-----------------|--------------------|
| `TotalRecord` | `total_record`  | จำนวนแถวทั้งหมด    |
| `TotalPage`   | `total_page`    | จำนวนหน้าทั้งหมด   |
| `Records`     | `data`          | slice ของผลลัพธ์   |
| `Offset`      | `offset`        | offset ปัจจุบัน     |
| `PerPage`     | `per_page`      | จำนวนต่อหน้า        |
| `Page`        | `page`          | หน้าปัจจุบัน         |
| `PrevPage`    | `prev_page`     | หน้าก่อน            |
| `NextPage`    | `next_page`     | หน้าถัดไป           |

---

## Functions

### PagingCH

```go
func PagingCH(p *PagingConfigCH, result interface{}) (*PaginatorCH, error)
```

- รัน query แบบมี paging แล้วเติมผลลง `result`
- `result` ต้องเป็น pointer ไป slice ของ struct เช่น `*[]MyModel`
- ลำดับ: `Count()` → ใส่ ORDER BY ตาม `OrderBy` → ถ้าไม่ใช่ `All` ใส่ LIMIT/OFFSET → `Find()`

### GeneratePagingOrderCH

```go
func GeneratePagingOrderCH(args ...string) *PagingOrderByCH
```

สร้าง `PagingOrderByCH` จาก request (เหมือน `helpers.GeneratePagingOrder`):

- `args[0]` = sortBy จาก request, default `"created_at"`
- `args[1]` = sortType จาก request, default `"desc"`
- `args[2]` = sortBy default (optional)
- `args[3]` = sortType default (optional)

---

## Usage

### พื้นฐาน

```go
import (
    ClickHouseORM "github.com/hexlinevault/core-api.git/helpers/clickhouse"
    "github.com/doug-martin/goqu/v9"
)

var result []MyModel
q := ch.Model(&MyModel{}).Where(goqu.Ex{"tenant_id": tenantID})

paginator, err := ClickHouseORM.PagingCH(&ClickHouseORM.PagingConfigCH{
    Query:   q,
    Page:    1,
    PerPage: 20,
    Ctx:     ctx,
}, &result)
if err != nil {
    return err
}
// result, paginator.TotalRecord, paginator.TotalPage, ...
```

### มี Sort (OrderBy)

```go
order := ClickHouseORM.GeneratePagingOrderCH(req.SortBy, req.SortType)

paginator, err := ClickHouseORM.PagingCH(&ClickHouseORM.PagingConfigCH{
    Query:   q,
    Page:    page,
    PerPage: 20,
    OrderBy: []*ClickHouseORM.PagingOrderByCH{order},
    Ctx:     ctx,
}, &result)
```

### ดึงทั้งหมด (ไม่แบ่งหน้า)

```go
paginator, err := ClickHouseORM.PagingCH(&ClickHouseORM.PagingConfigCH{
    Query: q,
    All:   true,
    Ctx:   ctx,
}, &result)
// paginator.PerPage = total, paginator.TotalPage = 1
```

### ใช้ร่วมกับ helpers.Paginator

ถ้า handler ใช้ type เดียวกับ GORM paging:

```go
chPaginator, err := ClickHouseORM.PagingCH(&config, &result)
if err != nil {
    return err
}
// ใช้เป็น helpers.Paginator ได้
helpersPaginator := chPaginator.ToHelpersPaginator()
```

### Meta สำหรับ API

```go
meta := paginator.ToMap()
// map[string]interface{}{ "page", "per_page", "page_count", "total_count" }
```

---

## Safety

- **OrderBy** – คอลัมน์ใน `SortBy` ถูกเช็คด้วย regex `^[a-zA-Z0-9_.]+$` เท่านั้น
- **SortType** – รับเฉพาะ `asc`/`desc` (case-insensitive) นอกนั้นใช้ ASC

---

## เปรียบเทียบกับ helpers.Paging (GORM)

| Feature        | helpers.Paging | ClickHouse PagingCH |
|----------------|----------------|---------------------|
| Query type     | `*gorm.DB`     | `*Query` (goqu)     |
| Context        | ไม่ใช้          | ใช้ `Ctx` ใน config |
| Count          | GORM Count     | `Query.Count(ctx)`  |
| Fetch          | Find/Scan      | `Query.Find(ctx)`   |
| OrderBy        | clause.OrderBy | `PagingOrderByCH` + safe column name |
| Response shape | `Paginator`    | `PaginatorCH` (และ `ToHelpersPaginator()`) |
