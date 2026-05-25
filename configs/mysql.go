package configs

import (
	"time"

	"gorm.io/driver/mysql"
)

type MySQLConn struct {
	mysql.Config
	ConnectionName  string // empty is default
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxIdleTime *time.Duration
	ConnMaxLifetime *time.Duration
}
