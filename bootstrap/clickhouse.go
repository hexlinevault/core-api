package bootstrap

import (
	"context"
	"time"

	"github.com/hexlinevault/core-api.git/configs"
	chorm "github.com/hexlinevault/core-api.git/helpers/clickhouse"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

type ClickHouse struct {
}

var dbClickHouse map[string]clickhouse.Conn = make(map[string]clickhouse.Conn)

func CreateClickHouseConnection(conf *configs.ClickHouseConn) clickhouse.Conn {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})

	conn, err := clickhouse.Open(&conf.Options)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "clickhouse").Fatal("Failed to connect database")
	}

	if err := conn.Ping(context.Background()); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			Logger(context.Background()).WithError(err).WithField("code", exception.Code).WithField("connection_name", connectionName).WithField("component", "clickhouse").Fatal("Failed to ping database")
		}
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "clickhouse").Fatal("Failed to ping database")
	}

	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "clickhouse").Info("Database connected")

	dbClickHouse[connectionName] = conn
	return conn
}

func (c *ClickHouse) DB(connectionNames ...string) clickhouse.Conn {
	connectionName := resolveConnectionName(connectionNames)
	return dbClickHouse[connectionName]
}

func (c *ClickHouse) ORM(connectionNames ...string) *chorm.ClickHouseORM {
	connectionName := resolveConnectionName(connectionNames)
	return chorm.New(dbClickHouse[connectionName])
}

func (c *ClickHouse) HealthCheck(connectionNames ...string) string {
	connectionName := resolveConnectionName(connectionNames)
	if dbClickHouse[connectionName] == nil {
		return "connection not found"
	}
	err := dbClickHouse[connectionName].Ping(context.Background())
	if err != nil {
		return "error " + err.Error()
	} else {
		return "ok"
	}
}

func (c *ClickHouse) HealthCheckWithResponse(connectionNames ...string) (string, time.Duration) {
	start := time.Now()
	connectionName := resolveConnectionName(connectionNames)
	if dbClickHouse[connectionName] == nil {
		return "connection not found", 0
	}
	err := dbClickHouse[connectionName].Ping(context.Background())
	elapsed := duration("clickhouse", start)
	if err != nil {
		return "error " + err.Error(), elapsed
	} else {
		return "ok", elapsed
	}
}
