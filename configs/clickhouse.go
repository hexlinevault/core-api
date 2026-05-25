package configs

import (
	"github.com/ClickHouse/clickhouse-go/v2"
)

type ClickHouseConn struct {
	clickhouse.Options
	ConnectionName string // empty is default
}
