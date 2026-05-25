package bootstrap

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/hexlinevault/core-api/configs"

	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type (
	// ClickHouse clickhouse database management
	ClickHouseGorm struct {
	}
)

// dbClickHouse variable for define connection
var dbClickHouseGorm map[string]*gorm.DB = make(map[string]*gorm.DB)

// CreateClickHouseConnection make connection
// example
// connection := fmt.Sprintf("clickhouse://%s:%s@%s:%s/%s?dial_timeout=10s&read_timeout=20s",
//
//	os.Getenv("CLICKHOUSE_USERNAME"),
//	os.Getenv("CLICKHOUSE_PASSWORD"),
//	os.Getenv("CLICKHOUSE_HOST"),
//	os.Getenv("CLICKHOUSE_PORT"),
//	os.Getenv("CLICKHOUSE_DBNAME"),
//
// )
//
//	bootstraps.CreateClickHouseConnection(&configs.ClickHouseConn{
//		Config: clickhouse.Config{
//	  DSN: connection,
//	 },
//	})
//
// new(bootstraps.ClickHouse).DB()
func CreateClickHouseConnectionGorm(conf *configs.ClickHouseGormConn) *gorm.DB {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})

	// Determine log level based on APP_DEBUG
	logLevel := logger.Silent
	if debug, err := strconv.ParseBool(os.Getenv("APP_DEBUG")); err == nil && debug {
		logLevel = logger.Info
	}

	// Create custom GORM logger using our centralized logger (reusing gormLoggerAdapter from mysql.go)
	gormLogger := &gormLoggerAdapter{component: "clickhouse", connectionName: connectionName, logLevel: logLevel}

	db, err := gorm.Open(clickhouse.Open(conf.DSN), &gorm.Config{
		Logger: gormLogger,
	})

	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "clickhouse").Fatal("Failed to connect database")
	}
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "clickhouse").Info("Database connected")

	if c, err := db.DB(); err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "clickhouse").Fatal("Connection pool error")
	} else {
		if v := conf.MaxIdleConns; v > 0 {
			c.SetMaxIdleConns(v)
		}
		if v := conf.MaxOpenConns; v > 0 {
			c.SetMaxOpenConns(v)
		}
		if v := conf.ConnMaxIdleTime; v != nil {
			c.SetConnMaxIdleTime(*v)
		}
		if v := conf.ConnMaxLifetime; v != nil {
			c.SetConnMaxLifetime(*v)
		}
	}
	dbClickHouseGorm[connectionName] = db
	return db
}

// DB get clickhouse connection
func (c *ClickHouseGorm) DB(connectionNames ...string) *gorm.DB {
	connectionName := resolveConnectionName(connectionNames)
	return dbClickHouseGorm[connectionName]
}

func (c *ClickHouseGorm) HealthCheck(connectionNames ...string) string {
	connectionName := resolveConnectionName(connectionNames)
	err := dbClickHouseGorm[connectionName].Exec("SELECT 1").Error
	if err != nil {
		return "error " + err.Error()
	} else {
		return "ok"
	}
}

func (c *ClickHouseGorm) HealthCheckWithResponse(connectionNames ...string) (string, time.Duration) {
	start := time.Now()
	connectionName := resolveConnectionName(connectionNames)
	err := dbClickHouseGorm[connectionName].Exec("SELECT 1").Error
	time := duration("clickhouse", start)
	responseTime := time
	if err != nil {
		return "error " + err.Error(), responseTime
	} else {
		return "ok", responseTime
	}
}
