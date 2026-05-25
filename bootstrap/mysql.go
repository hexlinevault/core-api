package bootstrap

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/hexlinevault/core-api.git/configs"

	mysql "go.elastic.co/apm/module/apmgormv2/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type (
	// MySQL mysql database management
	MySQL struct {
	}

	// gormLoggerAdapter adapts our centralized logger to GORM logger interface
	gormLoggerAdapter struct {
		component      string
		connectionName string
		logLevel       logger.LogLevel
	}
)

// dbMySQL variable for define connection
var dbMySQL map[string]*gorm.DB = make(map[string]*gorm.DB)

// Printf implements GORM logger interface (required for Writer interface)
func (l *gormLoggerAdapter) Printf(format string, args ...interface{}) {
	Logger(context.Background()).WithField("component", l.component).WithField("connection_name", l.connectionName).Infof(format, args...)
}

// LogMode implements GORM logger interface
func (l *gormLoggerAdapter) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *l
	newLogger.logLevel = level
	return &newLogger
}

// Info implements GORM logger interface
func (l *gormLoggerAdapter) Info(ctx context.Context, msg string, data ...interface{}) {
	Logger(ctx).WithField("component", l.component).WithField("connection_name", l.connectionName).Infof(msg, data...)
}

// Warn implements GORM logger interface
func (l *gormLoggerAdapter) Warn(ctx context.Context, msg string, data ...interface{}) {
	Logger(ctx).WithField("component", l.component).WithField("connection_name", l.connectionName).Warnf(msg, data...)
}

// Error implements GORM logger interface
func (l *gormLoggerAdapter) Error(ctx context.Context, msg string, data ...interface{}) {
	Logger(ctx).WithField("component", l.component).WithField("connection_name", l.connectionName).Errorf(msg, data...)
}

// Trace implements GORM logger interface
func (l *gormLoggerAdapter) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	logEntry := Logger(ctx).WithField("component", l.component).WithField("connection_name", l.connectionName)

	if err != nil {
		logEntry.WithError(err).WithField("duration", elapsed).WithField("rows", rows).Error("SQL error")
	} else if elapsed > 100*time.Millisecond {
		logEntry.WithField("duration", elapsed).WithField("rows", rows).WithField("sql", sql).Warn("Slow query")
	} else if l.logLevel >= logger.Info {
		// Log all queries when in debug mode (Info level or higher)
		logEntry.WithField("duration", elapsed).WithField("rows", rows).Infof("%s", sql)
	}
}

// CreateMySQLConnection make connection
// example
// connection := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8&parseTime=True&loc=Local",
//
//	os.Getenv("MYSQL_USERNAME"),
//	os.Getenv("MYSQL_PASSWORD"),
//	os.Getenv("MYSQL_HOST"),
//	os.Getenv("MYSQL_PORT"),
//	os.Getenv("MYSQL_DBNAME"),
//
// )
//
//	bootstraps.CreateMySQLConnection(&configs.MySQLConn{
//		Config: mysql.Config{
//	  DSN: connection,
//	 },
//	})
//
// new(bootstraps.MySQL).DB()
//
//	bootstraps.CreateMySQLConnection(&configs.MySQLConn{
//		Config: mysql.Config{
//	  DSN: connection,
//	 },
//		ConnectionName: "staging"
//	})
//
// new(bootstraps.MySQL).DB("staging")
// })
func CreateMySQLConnection(conf *configs.MySQLConn) *gorm.DB {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})

	// Determine log level based on APP_DEBUG
	// Use Info level instead of db.Debug() to ensure Trace method is called (which receives context)
	// db.Debug() uses Printf which doesn't receive context, breaking correlation_id
	logLevel := logger.Silent
	if debug, err := strconv.ParseBool(os.Getenv("APP_DEBUG")); err == nil && debug {
		logLevel = logger.Info
	}

	// Create custom GORM logger using our centralized logger
	gormLogger := &gormLoggerAdapter{component: "mysql", connectionName: connectionName, logLevel: logLevel}

	db, err := gorm.Open(mysql.Open(conf.DSN), &gorm.Config{
		Logger: gormLogger,
	})

	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "mysql").Fatal("Failed to connect database")
	}
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "mysql").Info("Database connected")

	if c, err := db.DB(); err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "mysql").Fatal("Connection pool error")
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
	dbMySQL[connectionName] = db
	return db
}

// DB get mysql connection
func (c *MySQL) DB(connectionNames ...string) *gorm.DB {
	connectionName := resolveConnectionName(connectionNames)
	return dbMySQL[connectionName]
}

func (c *MySQL) HealthCheck(connectionNames ...string) string {
	connectionName := resolveConnectionName(connectionNames)
	err := dbMySQL[connectionName].Exec("SELECT 1").Error
	if err != nil {
		return "error " + err.Error()
	} else {
		return "ok"
	}
}

func (c *MySQL) HealthCheckWithResponse(connectionNames ...string) (string, time.Duration) {
	start := time.Now()
	connectionName := resolveConnectionName(connectionNames)
	err := dbMySQL[connectionName].Exec("SELECT 1").Error
	time := duration("mysql", start)
	responseTime := time
	if err != nil {
		return "error " + err.Error(), responseTime
	} else {
		return "ok", responseTime
	}
}

func duration(msg string, start time.Time) time.Duration {
	return time.Since(start)
}
