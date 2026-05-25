// Package ClickHouseORM provides an ORM-lite layer for ClickHouse using clickhouse-go/v2 (Native)
// plus a goqu-based query builder.
//
// Goals:
// - GORM-like ergonomics for SELECT/ClickHouseORMrting and batch INSERTs
// - Full SQL control (ClickHouse-appropriate)
// - Safe parameter binding (use ? placeholders via goqu.L / WhereRaw / SelectRawMulti)
//
// Non-goals (because ClickHouse is OLAP):
// - row-level transactions like OLTP
// - per-row UPDATE patterns (prefer insert-only + MV + snapshots)
package ClickHouseORM

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/doug-martin/goqu/v9"
)

var ErrNoRows = errors.New("ClickHouseORM: no rows found")

// ---------- Core types ----------

type TableNamer interface {
	TableName() string
}

type DebugLogger interface {
	Printf(format string, v ...any)
}

type ClickHouseORM struct {
	Conn   clickhouse.Conn
	D      *goqu.Database
	Logger DebugLogger
}

func New(conn clickhouse.Conn) *ClickHouseORM {
	d := goqu.New("default", nil)
	return &ClickHouseORM{Conn: conn, D: d}
}

func (r *ClickHouseORM) WithDebug(logger DebugLogger) *ClickHouseORM {
	n := *r
	if logger == nil {
		logger = log.Default()
	}
	n.Logger = logger
	return &n
}

func (r *ClickHouseORM) Exec(ctx context.Context, sql string, args ...any) error {
	if r.Logger != nil {
		r.Logger.Printf("CH SQL: %s | args=%v", sql, args)
	}
	return r.Conn.Exec(ctx, sql, args...)
}

func (r *ClickHouseORM) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	if r.Logger != nil {
		r.Logger.Printf("CH SQL: %s | args=%v", sql, args)
	}
	return r.Conn.QueryRow(ctx, sql, args...)
}

// ---------- Query builder ----------

type Query struct {
	ClickHouseORM *ClickHouseORM
	from          string
	modelType     reflect.Type
	ds            *goqu.SelectDataset
}

// Model creates a query using the model's TableName and auto-selects columns from its db tags.
func (r *ClickHouseORM) Model(m TableNamer) *Query {
	t := reflect.TypeOf(m)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	cols, err := columnsFromType(t)
	if err != nil {
		cols = []string{"*"}
	}
	ds := r.D.From(m.TableName()).Select(stringSliceToIdentifiers(cols)...).Prepared(true)
	return &Query{ClickHouseORM: r, from: m.TableName(), modelType: t, ds: ds}
}

// From creates a query with an explicit table name and SELECT *.
func (r *ClickHouseORM) From(table string) *Query {
	ds := r.D.From(table).Select(goqu.Star()).Prepared(true)
	return &Query{ClickHouseORM: r, from: table, ds: ds}
}

func (q *Query) logSQL(sql string, args []any) {
	if q.ClickHouseORM != nil && q.ClickHouseORM.Logger != nil {
		q.ClickHouseORM.Logger.Printf("CH SQL: %s | args=%v", sql, args)
	}
}

func (q *Query) ToSQL() (string, []any, error) {
	return q.ds.Prepared(true).ToSQL()
}

func (q *Query) DebugSQL() (string, []any, error) { return q.ToSQL() }

func (q *Query) DebugPrint() {
	sqlStr, args, err := q.ToSQL()
	if err != nil {
		fmt.Println("debugsql error:", err)
		return
	}
	fmt.Println("SQL:", sqlStr)
	fmt.Println("ARGS:", args)
}

// ---------- Builder / chain methods ----------

func (q *Query) Select(cols ...any) *Query {
	q.ds = q.ClickHouseORM.D.From(q.from).Select(cols...).Prepared(true)
	return q
}

func (q *Query) SelectCols(cols ...string) *Query {
	exprs := make([]any, 0, len(cols))
	for _, c := range cols {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if looksLikeExpr(c) {
			exprs = append(exprs, goqu.L(c))
		} else {
			exprs = append(exprs, goqu.I(c))
		}
	}
	q.ds = q.ds.ClearSelect().Select(exprs...)
	return q
}

func (q *Query) Where(ex goqu.Ex) *Query {
	q.ds = q.ds.Where(ex)
	return q
}

func (q *Query) WhereSQL(sql string, args ...any) *Query {
	return q.WhereRaw(sql, args...)
}

func (q *Query) WhereRaw(sql string, args ...any) *Query {
	q.ds = q.ds.Where(goqu.L(sql, args...))
	return q
}

func (q *Query) WhereIn(col string, values ...any) *Query {
	q.ds = q.ds.Where(goqu.I(col).In(values...))
	return q
}

func (q *Query) BetweenTime(col string, from, to time.Time) *Query {
	q.ds = q.ds.Where(
		goqu.And(
			goqu.I(col).Gte(from),
			goqu.I(col).Lt(to),
		),
	)
	return q
}

func (q *Query) GroupBy(cols ...string) *Query {
	idents := make([]any, 0, len(cols))
	for _, c := range cols {
		idents = append(idents, goqu.I(c))
	}
	q.ds = q.ds.GroupBy(idents...)
	return q
}

func (q *Query) OrderBy(order ...string) *Query {
	for _, o := range order {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		upper := strings.ToUpper(o)
		if strings.HasSuffix(upper, " DESC") {
			q.ds = q.ds.OrderAppend(goqu.L(strings.TrimSpace(o[:len(o)-5])).Desc())
		} else if strings.HasSuffix(upper, " ASC") {
			q.ds = q.ds.OrderAppend(goqu.L(strings.TrimSpace(o[:len(o)-4])).Asc())
		} else {
			q.ds = q.ds.OrderAppend(goqu.L(o).Asc())
		}
	}
	return q
}

func (q *Query) OrderBySafe(allowedCols map[string]bool, order ...string) *Query {
	for _, o := range order {
		parts := strings.Fields(o)
		if len(parts) == 0 {
			continue
		}
		col := parts[0]
		if !allowedCols[col] {
			continue
		}
		if len(parts) == 1 {
			q.ds = q.ds.OrderAppend(goqu.I(col).Asc())
			continue
		}
		dir := strings.ToUpper(parts[1])
		if dir == "DESC" {
			q.ds = q.ds.OrderAppend(goqu.I(col).Desc())
		} else {
			q.ds = q.ds.OrderAppend(goqu.I(col).Asc())
		}
	}
	return q
}

func (q *Query) Limit(n uint) *Query {
	q.ds = q.ds.Limit(n)
	return q
}

func (q *Query) Offset(n uint) *Query {
	q.ds = q.ds.Offset(n)
	return q
}

func (q *Query) Page(page, size uint) *Query {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 50
	}
	q.Limit(size)
	q.Offset((page - 1) * size)
	return q
}

func (q *Query) HavingRaw(sql string, args ...any) *Query {
	q.ds = q.ds.Having(goqu.L(sql, args...))
	return q
}

// Final rewrites the FROM clause to "table FINAL", forcing ClickHouse
// ReplacingMergeTree to deduplicate rows before returning results.
func (q *Query) Final() *Query {
	q.ds = q.ds.From(goqu.L(q.from + " FINAL"))
	return q
}

// ---------- Join helpers ----------

func (q *Query) JoinRaw(joinSQL string, args ...any) *Query {
	if idx := strings.Index(strings.ToUpper(joinSQL), " ON "); idx >= 0 {
		table := strings.TrimSpace(joinSQL[:idx])
		cond := strings.TrimSpace(joinSQL[idx+4:])
		q.ds = q.ds.InnerJoin(goqu.L(table), goqu.On(goqu.L(cond, args...)))
	} else {
		q.ds = q.ds.CrossJoin(goqu.L(joinSQL, args...))
	}
	return q
}

func (q *Query) JoinOn(joinType string, table string, onSQL string, args ...any) *Query {
	cond := goqu.On(goqu.L(onSQL, args...))
	tbl := goqu.T(table)
	switch strings.TrimSpace(strings.ToUpper(joinType)) {
	case "LEFT", "ANY LEFT":
		q.ds = q.ds.LeftJoin(tbl, cond)
	case "RIGHT", "ANY RIGHT":
		q.ds = q.ds.RightJoin(tbl, cond)
	case "FULL", "FULL OUTER":
		q.ds = q.ds.FullJoin(tbl, cond)
	case "CROSS":
		q.ds = q.ds.CrossJoin(tbl)
	default:
		q.ds = q.ds.InnerJoin(tbl, cond)
	}
	return q
}

func (q *Query) LeftJoinAny(table string, onSQL string, args ...any) *Query {
	return q.JoinOn("ANY LEFT", table, onSQL, args...)
}

func (q *Query) InnerJoin(table string, onSQL string, args ...any) *Query {
	return q.JoinOn("INNER", table, onSQL, args...)
}

// ---------- Raw select helpers ----------

type RawExpr struct {
	SQL  string
	Args []any
}

func Expr(sql string, args ...any) RawExpr { return RawExpr{SQL: sql, Args: args} }

func (q *Query) SelectRaw(sql string, args ...any) *Query {
	q.ds = q.ds.ClearSelect().Select(goqu.L(sql, args...))
	return q
}

func (q *Query) SelectExprs(exprs ...any) *Query {
	q.ds = q.ds.ClearSelect().Select(exprs...)
	return q
}

func (q *Query) SelectRawMulti(exprs ...RawExpr) *Query {
	sels := make([]any, 0, len(exprs))
	for _, e := range exprs {
		sels = append(sels, goqu.L(e.SQL, e.Args...))
	}
	q.ds = q.ds.ClearSelect().Select(sels...)
	return q
}

func (q *Query) SelectColsArgs(sql string, args ...any) *Query {
	q.ds = q.ds.ClearSelect().Select(goqu.L(sql, args...))
	return q
}

func (q *Query) SelectRawSplit(sql1 string, args1 []any, more ...any) *Query {
	sels := []any{goqu.L(sql1, args1...)}
	if len(more)%2 != 0 {
		more = more[:len(more)-1]
	}
	for i := 0; i < len(more); i += 2 {
		s, _ := more[i].(string)
		a, _ := more[i+1].([]any)
		if strings.TrimSpace(s) == "" {
			continue
		}
		sels = append(sels, goqu.L(s, a...))
	}
	q.ds = q.ds.ClearSelect().Select(sels...)
	return q
}

// ---------- Fetch / Scan ----------

// scanAll executes the query and scans all rows into a reflect.Value slice of elemType.
func (q *Query) scanAll(ctx context.Context, elemType reflect.Type) (reflect.Value, error) {
	sqlStr, args, err := q.ToSQL()
	if err != nil {
		return reflect.Value{}, err
	}
	q.logSQL(sqlStr, args)

	rows, err := q.ClickHouseORM.Conn.Query(ctx, sqlStr, args...)
	if err != nil {
		return reflect.Value{}, err
	}
	defer rows.Close()

	actualCols := rows.Columns()
	if len(actualCols) == 0 {
		return reflect.Value{}, errors.New("query returned no columns")
	}

	result := reflect.MakeSlice(reflect.SliceOf(elemType), 0, 128)
	for rows.Next() {
		itemPtr := reflect.New(elemType)
		ptrs, postScan, err := scanPointersByResultColumns(itemPtr.Interface(), actualCols)
		if err != nil {
			return reflect.Value{}, err
		}
		if err := rows.Scan(ptrs...); err != nil {
			return reflect.Value{}, err
		}
		if err := postScan(); err != nil {
			return reflect.Value{}, err
		}
		result = reflect.Append(result, itemPtr.Elem())
	}
	if err := rows.Err(); err != nil {
		return reflect.Value{}, err
	}
	return result, nil
}

// Find scans query results into dest.
// dest must be a pointer to a slice of structs (*[]T) or a pointer to a struct (*T).
// When dest is *T, LIMIT 1 is applied automatically.
func (q *Query) Find(ctx context.Context, dest any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return fmt.Errorf("dest must be non-nil pointer")
	}

	elem := dv.Elem()
	switch elem.Kind() {
	case reflect.Slice:
		sliceElemType := elem.Type().Elem()
		wantPtr := sliceElemType.Kind() == reflect.Pointer
		elemType := sliceElemType
		if wantPtr {
			elemType = sliceElemType.Elem()
		}
		if elemType.Kind() != reflect.Struct {
			return fmt.Errorf("dest slice element must be struct")
		}
		result, err := q.scanAll(ctx, elemType)
		if err != nil {
			return err
		}
		if wantPtr {
			// dest is *[]*T; result is []T — build []*T and set
			out := reflect.MakeSlice(elem.Type(), result.Len(), result.Len())
			for i := 0; i < result.Len(); i++ {
				out.Index(i).Set(result.Index(i).Addr())
			}
			elem.Set(out)
		} else {
			elem.Set(result)
		}
		return nil

	case reflect.Struct:
		q.Limit(1)
		result, err := q.scanAll(ctx, elem.Type())
		if err != nil {
			return err
		}
		if result.Len() == 0 {
			return ErrNoRows
		}
		elem.Set(result.Index(0))
		return nil
	}

	return fmt.Errorf("dest must be pointer to struct or slice of struct")
}

// First applies LIMIT 1 and scans one row into dest (*Struct).
func (q *Query) First(ctx context.Context, dest any) error {
	q.Limit(1)
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return fmt.Errorf("dest must be non-nil pointer to struct")
	}
	elemType := dv.Elem().Type()
	if elemType.Kind() != reflect.Struct {
		return fmt.Errorf("dest must be pointer to struct")
	}
	result, err := q.scanAll(ctx, elemType)
	if err != nil {
		return err
	}
	if result.Len() == 0 {
		return ErrNoRows
	}
	dv.Elem().Set(result.Index(0))
	return nil
}

// Take clears ORDER BY, applies LIMIT 1, and scans one row into dest (*Struct).
func (q *Query) Take(ctx context.Context, dest any) error {
	q.ds = q.ds.ClearOrder()
	return q.First(ctx, dest)
}

func (q *Query) Count(ctx context.Context) (uint64, error) {
	ds := q.ds.ClearSelect().ClearOrder().Limit(0).Offset(0).Select(goqu.L("count()"))
	sqlStr, args, err := ds.Prepared(true).ToSQL()
	if err != nil {
		return 0, err
	}
	q.logSQL(sqlStr, args)

	var n uint64
	if err := q.ClickHouseORM.Conn.QueryRow(ctx, sqlStr, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (q *Query) Exists(ctx context.Context) (bool, error) {
	ds := q.ds.ClearSelect().ClearOrder().Limit(1).Offset(0).Select(goqu.L("1"))
	sqlStr, args, err := ds.Prepared(true).ToSQL()
	if err != nil {
		return false, err
	}
	q.logSQL(sqlStr, args)

	rows, err := q.ClickHouseORM.Conn.Query(ctx, sqlStr, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	ok := rows.Next()
	return ok, rows.Err()
}

// Pluck extracts a single column into destSlice (must be *[]T, e.g. *[]uint64).
func (q *Query) Pluck(ctx context.Context, selectExpr any, destSlice any) error {
	sqlStr, args, err := q.ds.ClearSelect().Select(selectExpr).Prepared(true).ToSQL()
	if err != nil {
		return err
	}
	q.logSQL(sqlStr, args)

	rows, err := q.ClickHouseORM.Conn.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	dv := reflect.ValueOf(destSlice)
	if dv.Kind() != reflect.Pointer || dv.IsNil() || dv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("destSlice must be non-nil pointer to slice")
	}

	sliceV := dv.Elem()
	elemT := sliceV.Type().Elem()

	for rows.Next() {
		elemPtr := reflect.New(elemT)
		if err := rows.Scan(elemPtr.Interface()); err != nil {
			return err
		}
		sliceV = reflect.Append(sliceV, elemPtr.Elem())
	}
	dv.Elem().Set(sliceV)
	return rows.Err()
}

// ScanInto scans into *struct or *[]struct with an explicit column order.
func (q *Query) ScanInto(ctx context.Context, dst any, cols []string) error {
	sqlStr, args, err := q.ToSQL()
	if err != nil {
		return err
	}
	q.logSQL(sqlStr, args)

	rows, err := q.ClickHouseORM.Conn.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return fmt.Errorf("dst must be non-nil pointer")
	}

	if dv.Elem().Kind() == reflect.Slice {
		sliceV := dv.Elem()
		elemT := sliceV.Type().Elem()
		if elemT.Kind() == reflect.Pointer {
			elemT = elemT.Elem()
		}
		if elemT.Kind() != reflect.Struct {
			return fmt.Errorf("dst slice element must be struct")
		}

		for rows.Next() {
			itemPtr := reflect.New(elemT)
			ptrs, postScan, err := scanPointersByCols(itemPtr, cols)
			if err != nil {
				return err
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			if err := postScan(); err != nil {
				return err
			}
			sliceV = reflect.Append(sliceV, itemPtr.Elem())
		}
		dv.Elem().Set(sliceV)
		return rows.Err()
	}

	if dv.Elem().Kind() == reflect.Struct {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return ErrNoRows
		}
		ptrs, postScan, err := scanPointersByCols(dv, cols)
		if err != nil {
			return err
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		return postScan()
	}

	return fmt.Errorf("dst must be pointer to struct or pointer to slice of struct")
}

// ---------- Scopes ----------

type Scope func(*Query) *Query

func (q *Query) Scopes(scopes ...Scope) *Query {
	for _, s := range scopes {
		if s != nil {
			q = s(q)
		}
	}
	return q
}

// ---------- Preload (GORM-like, WHERE IN + map) ----------

type HasManyOpt struct {
	ChildTable     string
	ChildKeyCol    string
	ParentKeyField string
	ChildKeyField  string
	AttachField    string
	Select         func(*Query) *Query
	Where          func(*Query) *Query
}

// PreloadHasMany loads child rows and attaches them to each parent.
// parents must be a pointer to a slice of structs (*[]Parent).
// The child type is inferred from the AttachField (which must be []Child).
func (r *ClickHouseORM) PreloadHasMany(ctx context.Context, parents any, opt HasManyOpt) error {
	pv := reflect.ValueOf(parents)
	if pv.Kind() != reflect.Pointer || pv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("parents must be *[]Struct")
	}
	ps := pv.Elem()
	if ps.Len() == 0 {
		return nil
	}

	parentType := ps.Type().Elem()
	attachField, ok := parentType.FieldByName(opt.AttachField)
	if !ok {
		return fmt.Errorf("field %q not found on parent struct", opt.AttachField)
	}
	if attachField.Type.Kind() != reflect.Slice {
		return fmt.Errorf("field %q must be a slice type", opt.AttachField)
	}
	childType := attachField.Type.Elem()

	keysMap := map[any]struct{}{}
	keysAny := make([]any, 0, ps.Len())
	for i := 0; i < ps.Len(); i++ {
		k := ps.Index(i).FieldByName(opt.ParentKeyField).Interface()
		if _, exists := keysMap[k]; exists {
			continue
		}
		keysMap[k] = struct{}{}
		keysAny = append(keysAny, k)
	}

	q := r.From(opt.ChildTable)
	q.modelType = childType
	if opt.Select != nil {
		q = opt.Select(q)
	}
	q = q.WhereIn(opt.ChildKeyCol, keysAny...)
	if opt.Where != nil {
		q = opt.Where(q)
	}

	children, err := q.scanAll(ctx, childType)
	if err != nil {
		return err
	}

	buckets := map[any]reflect.Value{}
	for i := 0; i < children.Len(); i++ {
		k := children.Index(i).FieldByName(opt.ChildKeyField).Interface()
		if _, exists := buckets[k]; !exists {
			buckets[k] = reflect.MakeSlice(attachField.Type, 0, 4)
		}
		buckets[k] = reflect.Append(buckets[k], children.Index(i))
	}

	for i := 0; i < ps.Len(); i++ {
		k := ps.Index(i).FieldByName(opt.ParentKeyField).Interface()
		if b, exists := buckets[k]; exists {
			ps.Index(i).FieldByName(opt.AttachField).Set(b)
		}
	}
	return nil
}

type HasOneOpt struct {
	ChildTable     string
	ChildKeyCol    string
	ParentKeyField string
	ChildKeyField  string
	AttachField    string
	Select         func(*Query) *Query
	Where          func(*Query) *Query
	PickField      string // Go struct field to compare when choosing between duplicates
	PickLatest     bool   // true = keep the larger/later value; false = keep the smaller/earlier
}

// PreloadHasOne loads one child row per parent and attaches it.
// parents must be *[]Parent. AttachField must be *Child.
func (r *ClickHouseORM) PreloadHasOne(ctx context.Context, parents any, opt HasOneOpt) error {
	pv := reflect.ValueOf(parents)
	if pv.Kind() != reflect.Pointer || pv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("parents must be *[]Struct")
	}
	ps := pv.Elem()
	if ps.Len() == 0 {
		return nil
	}

	parentType := ps.Type().Elem()
	attachField, ok := parentType.FieldByName(opt.AttachField)
	if !ok {
		return fmt.Errorf("field %q not found on parent struct", opt.AttachField)
	}
	if attachField.Type.Kind() != reflect.Pointer {
		return fmt.Errorf("field %q must be a pointer type (*Child)", opt.AttachField)
	}
	childType := attachField.Type.Elem()

	keysMap := map[any]struct{}{}
	keysAny := make([]any, 0, ps.Len())
	for i := 0; i < ps.Len(); i++ {
		k := ps.Index(i).FieldByName(opt.ParentKeyField).Interface()
		if _, exists := keysMap[k]; exists {
			continue
		}
		keysMap[k] = struct{}{}
		keysAny = append(keysAny, k)
	}

	q := r.From(opt.ChildTable)
	q.modelType = childType
	if opt.Select != nil {
		q = opt.Select(q)
	}
	q = q.WhereIn(opt.ChildKeyCol, keysAny...)
	if opt.Where != nil {
		q = opt.Where(q)
	}

	children, err := q.scanAll(ctx, childType)
	if err != nil {
		return err
	}

	m := map[any]reflect.Value{}
	has := map[any]bool{}
	for i := 0; i < children.Len(); i++ {
		child := children.Index(i)
		k := child.FieldByName(opt.ChildKeyField).Interface()
		if !has[k] {
			m[k] = child
			has[k] = true
			continue
		}
		if opt.PickField != "" {
			existing := m[k].FieldByName(opt.PickField)
			incoming := child.FieldByName(opt.PickField)
			if opt.PickLatest && isGreater(incoming, existing) {
				m[k] = child
			} else if !opt.PickLatest && isGreater(existing, incoming) {
				m[k] = child
			}
		} else {
			m[k] = child
		}
	}

	for i := 0; i < ps.Len(); i++ {
		k := ps.Index(i).FieldByName(opt.ParentKeyField).Interface()
		f := ps.Index(i).FieldByName(opt.AttachField)
		if has[k] {
			ptr := reflect.New(childType)
			ptr.Elem().Set(m[k])
			f.Set(ptr)
		} else {
			f.Set(reflect.Zero(attachField.Type))
		}
	}
	return nil
}

// ---------- Insert helpers ----------

func (r *ClickHouseORM) InsertOne(ctx context.Context, m TableNamer, row any) error {
	rv := reflect.ValueOf(row)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return errors.New("InsertOne: row must be a struct or *struct")
	}
	cols, err := columnsForInsert(rv.Type())
	if err != nil {
		return err
	}
	// valuesByDBTags expects a pointer (uses .Elem())
	var itemPtr any
	if reflect.ValueOf(row).Kind() == reflect.Pointer {
		itemPtr = row
	} else {
		p := reflect.New(rv.Type())
		p.Elem().Set(rv)
		itemPtr = p.Interface()
	}
	vals, err := valuesByDBTags(itemPtr, cols)
	if err != nil {
		return err
	}
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = placeholders[:len(placeholders)-1]
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", m.TableName(), strings.Join(cols, ","), placeholders)
	if r.Logger != nil {
		r.Logger.Printf("CH SQL: %s | args=%v", sqlStr, vals)
	}
	return r.Conn.Exec(ctx, sqlStr, vals...)
}

// InsertBatch performs a fast batch insert using PrepareBatch.
// m provides the table name; rows must be a slice of structs.
func (r *ClickHouseORM) InsertBatch(ctx context.Context, m TableNamer, rows any) error {
	rv, err := asSliceValue(rows)
	if err != nil {
		return err
	}
	if rv.Len() == 0 {
		return nil
	}
	cols, err := columnsForInsert(rv.Type().Elem())
	if err != nil {
		return err
	}
	return r.insertBatchCols(ctx, m, cols, rv)
}

// InsertBatchColumns performs a batch insert with an explicit column list.
func (r *ClickHouseORM) InsertBatchColumns(ctx context.Context, m TableNamer, cols []string, rows any) error {
	rv, err := asSliceValue(rows)
	if err != nil {
		return err
	}
	return r.insertBatchCols(ctx, m, cols, rv)
}

func (r *ClickHouseORM) insertBatchCols(ctx context.Context, m TableNamer, cols []string, rv reflect.Value) error {
	n := rv.Len()
	if n == 0 {
		return nil
	}
	if len(cols) == 0 {
		return fmt.Errorf("cols is empty")
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s)", m.TableName(), strings.Join(cols, ","))
	if r.Logger != nil {
		r.Logger.Printf("CH INSERT: %s | rows=%d", insertSQL, n)
	}

	batch, err := r.Conn.PrepareBatch(ctx, insertSQL)
	if err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		elem := rv.Index(i)
		var itemPtr any
		if elem.Kind() == reflect.Pointer {
			itemPtr = elem.Interface()
		} else {
			itemPtr = elem.Addr().Interface()
		}
		vals, err := valuesByDBTags(itemPtr, cols)
		if err != nil {
			return err
		}
		if err := batch.Append(vals...); err != nil {
			return err
		}
	}
	return batch.Send()
}

// ---------- Chunked insert + retry/backoff ----------

func (r *ClickHouseORM) InsertBatchChunked(
	ctx context.Context,
	m TableNamer,
	rows any,
	chunkSize int,
	retryMax int,
) error {
	rv, err := asSliceValue(rows)
	if err != nil {
		return err
	}
	n := rv.Len()
	if n == 0 {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 20000
	}
	if retryMax <= 0 {
		retryMax = 3
	}

	cols, err := columnsForInsert(rv.Type().Elem())
	if err != nil {
		return err
	}

	for start := 0; start < n; start += chunkSize {
		end := start + chunkSize
		if end > n {
			end = n
		}
		chunk := rv.Slice(start, end)

		if err := r.insertOneChunkWithRetry(ctx, m, cols, chunk, retryMax); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClickHouseORM) insertOneChunkWithRetry(ctx context.Context, m TableNamer, cols []string, chunk reflect.Value, retryMax int) error {
	var lastErr error
	for attempt := 0; attempt <= retryMax; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := r.insertBatchCols(ctx, m, cols, chunk)
		if err == nil {
			return nil
		}
		lastErr = err

		if !shouldRetryCH(err) || attempt == retryMax {
			return err
		}
		time.Sleep(backoffDuration(attempt))
	}
	return lastErr
}

func shouldRetryCH(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporarily") ||
		strings.Contains(msg, "too many") ||
		strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "server is busy") ||
		strings.Contains(msg, "try again") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof") {
		return true
	}
	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) {
		switch chErr.Code {
		case 159, 202, 209, 252, 319:
			return true
		default:
			return false
		}
	}
	return false
}

func backoffDuration(attempt int) time.Duration {
	base := 200 * time.Millisecond
	max := 5 * time.Second
	mult := math.Pow(2, float64(attempt))
	d := time.Duration(float64(base) * mult)
	if d > max {
		d = max
	}
	j := 0.7 + rand.Float64()*0.6
	return time.Duration(float64(d) * j)
}

// ---------- Parallel + dynamic chunking ----------

func (r *ClickHouseORM) InsertBatchChunkedParallelDynamic(
	ctx context.Context,
	m TableNamer,
	rows any,
	chunkSizeStart int,
	minChunk int,
	workers int,
	retryMax int,
	qps int,
) error {
	rv, err := asSliceValue(rows)
	if err != nil {
		return err
	}
	n := rv.Len()
	if n == 0 {
		return nil
	}
	if chunkSizeStart <= 0 {
		chunkSizeStart = 20000
	}
	if minChunk <= 0 {
		minChunk = 1000
	}
	if workers <= 0 {
		workers = 4
	}
	if retryMax <= 0 {
		retryMax = 4
	}

	cols, err := columnsForInsert(rv.Type().Elem())
	if err != nil {
		return err
	}

	type job struct {
		start int
		end   int
	}
	jobs := make(chan job, workers*2)
	errCh := make(chan error, 1)

	var tick <-chan time.Time
	if qps > 0 {
		ticker := time.NewTicker(time.Second / time.Duration(qps))
		defer ticker.Stop()
		tick = ticker.C
	}

	var wg sync.WaitGroup
	workerFn := func() {
		defer wg.Done()
		for j := range jobs {
			select {
			case <-ctx.Done():
				return
			default:
			}

			select {
			case e := <-errCh:
				select {
				case errCh <- e:
				default:
				}
				return
			default:
			}

			if tick != nil {
				select {
				case <-tick:
				case <-ctx.Done():
					return
				}
			}

			chunk := rv.Slice(j.start, j.end)
			if err := r.insertChunkDynamic(ctx, m, cols, chunk, minChunk, retryMax); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go workerFn()
	}

	for start := 0; start < n; start += chunkSizeStart {
		end := start + chunkSizeStart
		if end > n {
			end = n
		}

		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- job{start: start, end: end}:
		}
	}

	close(jobs)
	wg.Wait()

	select {
	case e := <-errCh:
		return e
	default:
		return nil
	}
}

func (r *ClickHouseORM) insertChunkDynamic(ctx context.Context, m TableNamer, cols []string, chunk reflect.Value, minChunk int, retryMax int) error {
	err := r.insertOneChunkWithRetry(ctx, m, cols, chunk, retryMax)
	if err == nil {
		return nil
	}
	if !shouldRetryCH(err) {
		return err
	}
	if chunk.Len() <= minChunk {
		return err
	}
	mid := chunk.Len() / 2
	if errL := r.insertChunkDynamic(ctx, m, cols, chunk.Slice(0, mid), minChunk, retryMax); errL != nil {
		return errL
	}
	return r.insertChunkDynamic(ctx, m, cols, chunk.Slice(mid, chunk.Len()), minChunk, retryMax)
}

// ---------- Utilities ----------

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func RecommendOptions(addr string, database, user, pass string, tlsCfg *tls.Config) *clickhouse.Options {
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &clickhouse.Options{
		Addr:     []string{addr},
		Protocol: clickhouse.Native,
		TLS:      tlsCfg,
		Auth: clickhouse.Auth{
			Database: database,
			Username: user,
			Password: pass,
		},
		DialTimeout:     5 * time.Second,
		MaxOpenConns:    50,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
	}
}

// ---------- Internal helpers ----------

func asSliceValue(rows any) (reflect.Value, error) {
	rv := reflect.ValueOf(rows)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("rows must be a slice, got %T", rows)
	}
	return rv, nil
}

func parseDBTag(tag string) (col string, isJSON bool, isMap bool, isSelectOnly bool) {
	parts := strings.Split(tag, ",")
	col = parts[0]
	for _, p := range parts[1:] {
		switch strings.TrimSpace(p) {
		case "json":
			isJSON = true
		case "map":
			isMap = true
		case "select", "select_only":
			isSelectOnly = true
		}
	}
	return
}

// fieldPath is a path of field indices into a struct (supports embedded structs).
type fieldPath struct {
	path         []int
	isJSON       bool
	isMap        bool
	isSelectOnly bool
}

// collectDBFields recursively collects db-tagged fields from t (including embedded structs).
// Order is depth-first: embedded first, then own fields.
func collectDBFields(t reflect.Type, prefix []int) (cols []string, meta map[string]fieldPath) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil
	}
	cols = make([]string, 0)
	meta = make(map[string]fieldPath)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("db")
		if tag == "" || strings.TrimSpace(tag) == "-" {
			// db:"-" = skip field (ignore), like GORM. Anonymous embedded struct: recurse.
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				subCols, subMeta := collectDBFields(f.Type, nil)
				for _, c := range subCols {
					if _, exists := meta[c]; !exists {
						cols = append(cols, c)
						p := subMeta[c].path
						fullPath := make([]int, 0, len(prefix)+1+len(p))
						fullPath = append(fullPath, prefix...)
						fullPath = append(fullPath, i)
						fullPath = append(fullPath, p...)
						meta[c] = fieldPath{path: fullPath, isJSON: subMeta[c].isJSON, isMap: subMeta[c].isMap, isSelectOnly: subMeta[c].isSelectOnly}
					}
				}
			} else if f.Anonymous && f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
				subCols, subMeta := collectDBFields(f.Type.Elem(), nil)
				for _, c := range subCols {
					if _, exists := meta[c]; !exists {
						cols = append(cols, c)
						p := subMeta[c].path
						fullPath := make([]int, 0, len(prefix)+1+len(p))
						fullPath = append(fullPath, prefix...)
						fullPath = append(fullPath, i)
						fullPath = append(fullPath, p...)
						meta[c] = fieldPath{path: fullPath, isJSON: subMeta[c].isJSON, isMap: subMeta[c].isMap, isSelectOnly: subMeta[c].isSelectOnly}
					}
				}
			}
			continue
		}
		col, isJSON, isMap, isSelectOnly := parseDBTag(tag)
		if col == "" || strings.TrimSpace(col) == "-" {
			continue
		}
		if _, exists := meta[col]; exists {
			continue
		}
		path := append(append([]int{}, prefix...), i)
		cols = append(cols, col)
		meta[col] = fieldPath{path: path, isJSON: isJSON, isMap: isMap, isSelectOnly: isSelectOnly}
	}
	return cols, meta
}

// fieldByPath returns the reflect.Value for the field at path (v must be the struct value).
func fieldByPath(v reflect.Value, path []int) reflect.Value {
	for _, i := range path {
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

func columnsFromType(t reflect.Type) ([]string, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, errors.New("type must be a struct")
	}
	cols, _ := collectDBFields(t, nil)
	if len(cols) == 0 {
		return nil, errors.New("no db tags found on struct")
	}
	return cols, nil
}

// columnsForInsert returns column names for INSERT, excluding fields tagged with db:"...,select" or db:"...,select_only".
func columnsForInsert(t reflect.Type) ([]string, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, errors.New("type must be a struct")
	}
	cols, meta := collectDBFields(t, nil)
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if !meta[c].isSelectOnly {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no insertable columns (all fields are select-only)")
	}
	return out, nil
}

func scanPointersByDBTags(itemPtr any, cols []string) (ptrs []any, postScan func() error, err error) {
	v := reflect.ValueOf(itemPtr).Elem()
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil, nil, errors.New("item must be struct")
	}

	_, colToMeta := collectDBFields(t, nil)

	type jsonPending struct {
		data   *string
		target reflect.Value
	}
	var jsonFields []jsonPending

	ptrs = make([]any, 0, len(cols))
	for _, c := range cols {
		fm, ok := colToMeta[c]
		if !ok {
			return nil, nil, fmt.Errorf("missing struct field for column %q", c)
		}
		fv := fieldByPath(v, fm.path)
		if fm.isJSON {
			s := new(string)
			ptrs = append(ptrs, s)
			jsonFields = append(jsonFields, jsonPending{data: s, target: fv})
		} else if fm.isMap {
			// Map/Array: driver fills map/slice directly
			ptrs = append(ptrs, fv.Addr().Interface())
		} else {
			ptrs = append(ptrs, fv.Addr().Interface())
		}
	}

	postScan = func() error {
		for _, jf := range jsonFields {
			if *jf.data == "" {
				continue
			}
			if err := json.Unmarshal([]byte(*jf.data), jf.target.Addr().Interface()); err != nil {
				return fmt.Errorf("json unmarshal field: %w", err)
			}
		}
		return nil
	}
	return ptrs, postScan, nil
}

// scanPointersByResultColumns builds scan destinations in the order of actualCols (from rows.Columns()).
// Columns present in the struct are scanned into struct fields; extra columns are scanned into a discard.
// This allows queries that return fewer or different columns than the full struct (e.g. explicit SelectCols).
func scanPointersByResultColumns(itemPtr any, actualCols []string) (ptrs []any, postScan func() error, err error) {
	v := reflect.ValueOf(itemPtr).Elem()
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil, nil, errors.New("item must be struct")
	}

	_, colToMeta := collectDBFields(t, nil)

	type jsonPending struct {
		data   *string
		target reflect.Value
	}
	var jsonFields []jsonPending

	ptrs = make([]any, 0, len(actualCols))
	for _, c := range actualCols {
		fm, ok := colToMeta[c]
		if !ok {
			ptrs = append(ptrs, new(interface{}))
			continue
		}
		fv := fieldByPath(v, fm.path)
		if fm.isJSON {
			s := new(string)
			ptrs = append(ptrs, s)
			jsonFields = append(jsonFields, jsonPending{data: s, target: fv})
		} else if fm.isMap {
			ptrs = append(ptrs, fv.Addr().Interface())
		} else {
			ptrs = append(ptrs, fv.Addr().Interface())
		}
	}

	postScan = func() error {
		for _, jf := range jsonFields {
			if *jf.data == "" {
				continue
			}
			if err := json.Unmarshal([]byte(*jf.data), jf.target.Addr().Interface()); err != nil {
				return fmt.Errorf("json unmarshal field: %w", err)
			}
		}
		return nil
	}
	return ptrs, postScan, nil
}

func scanPointersByCols(structPtr reflect.Value, cols []string) (ptrs []any, postScan func() error, err error) {
	v := structPtr.Elem()
	t := v.Type()

	_, colToMeta := collectDBFields(t, nil)

	type jsonPending struct {
		data   *string
		target reflect.Value
	}
	var jsonFields []jsonPending

	ptrs = make([]any, 0, len(cols))
	for _, c := range cols {
		fm, ok := colToMeta[c]
		if !ok {
			return nil, nil, fmt.Errorf("missing struct field for column %q", c)
		}
		fv := fieldByPath(v, fm.path)
		if fm.isJSON {
			s := new(string)
			ptrs = append(ptrs, s)
			jsonFields = append(jsonFields, jsonPending{data: s, target: fv})
		} else if fm.isMap {
			ptrs = append(ptrs, fv.Addr().Interface())
		} else {
			ptrs = append(ptrs, fv.Addr().Interface())
		}
	}

	postScan = func() error {
		for _, jf := range jsonFields {
			if *jf.data == "" {
				continue
			}
			if err := json.Unmarshal([]byte(*jf.data), jf.target.Addr().Interface()); err != nil {
				return fmt.Errorf("json unmarshal field: %w", err)
			}
		}
		return nil
	}
	return ptrs, postScan, nil
}

func valuesByDBTags(itemPtr any, cols []string) ([]any, error) {
	v := reflect.ValueOf(itemPtr).Elem()
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil, errors.New("item must be struct")
	}

	_, colToMeta := collectDBFields(t, nil)

	vals := make([]any, 0, len(cols))
	for _, c := range cols {
		fm, ok := colToMeta[c]
		if !ok {
			return nil, fmt.Errorf("missing struct field for column %q", c)
		}
		fv := fieldByPath(v, fm.path)
		switch {
		case fm.isMap:
			// ClickHouse Map/Array types: pass Go map/slice so the driver serializes correctly.
			switch fv.Kind() {
			case reflect.Map:
				if fv.IsNil() {
					vals = append(vals, reflect.MakeMap(fv.Type()).Interface())
				} else {
					vals = append(vals, fv.Interface())
				}
			case reflect.Slice, reflect.Array:
				if fv.IsNil() {
					vals = append(vals, reflect.MakeSlice(fv.Type(), 0, 0).Interface())
				} else {
					vals = append(vals, fv.Interface())
				}
			default:
				vals = append(vals, fv.Interface())
			}
		case fm.isJSON:
			// String column storing JSON (e.g. raw_bet, raw_response): marshal to string.
			data, err := json.Marshal(fv.Interface())
			if err != nil {
				return nil, fmt.Errorf("json marshal column %q: %w", c, err)
			}
			s := string(data)
			if s == "null" {
				s = "{}"
			}
			vals = append(vals, s)
		default:
			vals = append(vals, fv.Interface())
		}
	}
	return vals, nil
}

func stringSliceToIdentifiers(cols []string) []any {
	out := make([]any, 0, len(cols))
	for _, c := range cols {
		out = append(out, goqu.I(c))
	}
	return out
}

func looksLikeExpr(s string) bool {
	upper := strings.ToUpper(s)
	if strings.ContainsAny(s, " ()+-/*%,") {
		return true
	}
	if strings.Contains(upper, " AS ") || strings.Contains(upper, "CASE ") {
		return true
	}
	if strings.Contains(upper, "COUNT") || strings.Contains(upper, "SUM") ||
		strings.Contains(upper, "MAX") || strings.Contains(upper, "MIN") ||
		strings.Contains(upper, "ARGMAX") || strings.Contains(upper, "MERGE") {
		return true
	}
	return false
}

var timeType = reflect.TypeOf(time.Time{})

func isGreater(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() > b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return a.Uint() > b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() > b.Float()
	case reflect.String:
		return a.String() > b.String()
	default:
		if a.Type() == timeType {
			return a.Interface().(time.Time).After(b.Interface().(time.Time))
		}
		return false
	}
}
