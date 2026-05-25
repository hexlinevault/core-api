package configs

import (
	"time"

	"gorm.io/driver/postgres"
)

type PostgreSQLConn struct {
	postgres.Config
	ConnectionName  string // empty is default
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxIdleTime *time.Duration
	ConnMaxLifetime *time.Duration
}
