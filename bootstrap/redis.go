package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/hexlinevault/core-api.git/configs"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"
)

type (
	// Redis database management
	Redis struct {
	}
)

// dbRedis variable for define connection
var (
	redisMu sync.RWMutex
	dbRedis map[string]redis.UniversalClient = make(map[string]redis.UniversalClient)
	rs      map[string]*redsync.Redsync      = make(map[string]*redsync.Redsync)
)

// CreateRedisConnection make connection
// example
// database, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
//
//	bootstrap.CreateRedisConnection(&redis.UniversalOptions{
//	 Addrs:       strings.Split(os.Getenv("REDIS_HOST"), ","),
//	 Password:    os.Getenv("REDIS_PASSWORD"),
//	 DB:          database,
//	 DialTimeout: time.Duration(15) * time.Second,
//	 ConnectionName: "test" // empty is default
//	})
//
// new(bootstrap).DB().Get()....
// new(bootstrap).DB("test").Get()....
func CreateRedisConnection(conf *configs.RedisConn) redis.UniversalClient {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})
	var db redis.UniversalClient
	if !conf.IsRedisCluster {
		db = redis.NewUniversalClient(&conf.UniversalOptions)
	} else {
		c := &conf.UniversalOptions
		db = redis.NewClusterClient(c.Cluster())
	}

	// db.AddHook(apmgoredis.NewHook())
	if _, err := db.Ping(context.TODO()).Result(); err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "redis").Fatal("Database connection failed")
	}
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "redis").Info("Database connected")
	redisMu.Lock()
	dbRedis[connectionName] = db
	redisMu.Unlock()
	CreateRedSync(connectionName, db)
	return db
}

// DB get redis connection
func (c *Redis) DB(connectionNames ...string) redis.UniversalClient {
	connectionName := resolveConnectionName(connectionNames)
	redisMu.RLock()
	defer redisMu.RUnlock()
	return dbRedis[connectionName]
}

func (c *Redis) HealthCheck(connectionNames ...string) string {
	connectionName := resolveConnectionName(connectionNames)
	redisMu.RLock()
	db := dbRedis[connectionName]
	redisMu.RUnlock()
	_, err := db.Ping(context.TODO()).Result()
	if err != nil {
		return "error " + err.Error()
	}
	return "ok"
}

func (c *Redis) HealthCheckWithResponse(connectionNames ...string) (string, time.Duration) {
	start := time.Now()
	connectionName := resolveConnectionName(connectionNames)
	redisMu.RLock()
	db := dbRedis[connectionName]
	redisMu.RUnlock()
	_, err := db.Ping(context.TODO()).Result()
	elapsed := duration("redis", start)
	if err != nil {
		return "error " + err.Error(), elapsed
	}
	return "ok", elapsed
}

func CreateRedSync(connectionName string, c redis.UniversalClient) {
	pool := goredis.NewPool(c)
	redisMu.Lock()
	rs[connectionName] = redsync.New(pool)
	redisMu.Unlock()
}

func (c *Redis) RedSync(connectionNames ...string) *redsync.Redsync {
	connectionName := resolveConnectionName(connectionNames)
	redisMu.RLock()
	defer redisMu.RUnlock()
	return rs[connectionName]
}
