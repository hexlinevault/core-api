package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/hexlinevault/core-api.git/bootstrap"
)

type Block struct {
	redis                bootstrap.Redis
	name                 string
	times                int64
	expireDuration time.Duration
}

func (b *Block) Resource(name string, times int64, expireInSeconds int64) {
	b.name = fmt.Sprintf("block:%s", name)
	b.times = times
	b.expireDuration = time.Duration(expireInSeconds) * time.Second
}

func NewBlockResource(name string, times int64, expireInSeconds int64) *Block {
	b := new(Block)
	b.Resource(name, times, expireInSeconds)
	return b
}

func (b *Block) Exists(ct context.Context) (bool, time.Duration, error) {
	ctx, cancel := context.WithCancel(ct)
	defer cancel()
	number, err := b.redis.DB().IncrBy(ctx, b.name, 1).Result()
	if err != nil {
		return false, 0, err
	}
	var exp time.Duration
	if number > 0 {
		if number == 1 {
			b.redis.DB().PExpire(ctx, b.name, b.expireDuration).Result()
		}
		if number <= b.times {
			return false, 0, nil
		}
		ttl, err := b.redis.DB().TTL(ctx, b.name).Result()
		if err != nil {
			return false, 0, err
		}
		if ttl == -1 {
			b.redis.DB().Del(ctx, b.name)
		}
		exp = ttl
		return true, exp, nil
	}
	return true, exp, nil
}

func (b *Block) Destroy(ct context.Context) (bool, error) {
	ctx, cancel := context.WithCancel(ct)
	defer cancel()
	err := b.redis.DB().Del(ctx, b.name).Err()
	if err != nil {
		return false, err
	}
	return true, nil
}
