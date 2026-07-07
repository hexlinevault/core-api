package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/hexlinevault/core-api/configs"

	"github.com/lib/pq"
	postgres "go.elastic.co/apm/module/apmgormv2/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type (
	// Postgres PostgreSQL database management
	Postgres struct {
	}
)

// dbPostgres variable for define connection
var dbPostgres map[string]*gorm.DB = make(map[string]*gorm.DB)
var dbPostgresConnections map[string]string = make(map[string]string)

// CreatePostgresConnection make connection
// example
// connection := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
//
//	os.Getenv("POSTGRES_USERNAME"),
//	os.Getenv("POSTGRES_PASSWORD"),
//	os.Getenv("POSTGRES_HOST"),
//	os.Getenv("POSTGRES_PORT"),
//	os.Getenv("POSTGRES_DBNAME"),
//
// )
//
//	bootstraps.CreatePostgreSQLConnection(&configs.PostgreSQLConn{
//		Config: postgres.Config{
//	  DSN: connection,
//	 },
//	})
//
// new(bootstraps.Postgres).DB()
//
//	bootstraps.CreatePostgreSQLConnection(&configs.PostgreSQLConn{
//		Config: postgres.Config{
//	  DSN: connection,
//	 },
//		ConnectionName: "staging"
//	})
//
// new(bootstraps.Postgres).DB("staging")
func CreatePostgreSQLConnection(conf *configs.PostgreSQLConn) *gorm.DB {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})

	// Determine log level based on APP_DEBUG
	// Use Info level instead of db.Debug() to ensure Trace method is called (which receives context)
	// db.Debug() uses Printf which doesn't receive context, breaking correlation_id
	logLevel := logger.Silent
	if conf.Debug {
		logLevel = logger.Info
	}

	// Create custom GORM logger using our centralized logger
	gormLogger := &gormLoggerAdapter{component: "postgres", connectionName: connectionName, logLevel: logLevel}

	db, err := gorm.Open(postgres.Open(conf.DSN), &gorm.Config{
		Logger: gormLogger,
	})

	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "postgres").Fatal("Failed to connect database")
		panic(fmt.Sprintf("[postgres:%s] failed to connect database: %s", connectionName, err))
	}
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "postgres").Info("Database connected")

	if c, err := db.DB(); err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "postgres").Fatal("Connection pool error")
		panic(fmt.Sprintf("[postgres:%s] connection poll error: %s", connectionName, err))
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
	dbPostgres[connectionName] = db
	dbPostgresConnections[connectionName] = conf.DSN
	return db
}

// DB get postgresql connection
func (c *Postgres) DB(connectionNames ...string) *gorm.DB {
	connectionName := resolveConnectionName(connectionNames)
	return dbPostgres[connectionName]
}

func (c *Postgres) NewListener(connectionName string,
	minReconnectInterval time.Duration,
	maxReconnectInterval time.Duration,
	eventCallback pq.EventCallbackType) *pq.Listener {
	if name, ok := dbPostgresConnections[connectionName]; ok {
		return pq.NewListener(name, minReconnectInterval, maxReconnectInterval, eventCallback)
	} else {
		panic("invalid connection name")
	}
}
