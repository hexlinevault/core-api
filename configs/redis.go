package configs

import "github.com/redis/go-redis/v9"

type RedisConn struct {
	redis.UniversalOptions
	ConnectionName string //empty is default
	Prefix         string
	IsRedisCluster bool
}
