package configs

import (
	"time"

	"gorm.io/driver/clickhouse"
)

type ClickHouseGormConn struct {
	clickhouse.Config
	ConnectionName  string // empty is default
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxIdleTime *time.Duration
	ConnMaxLifetime *time.Duration
}
