package redis_test

import (
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/velmie/idempo"
	idemporedis "github.com/velmie/idempo/redis"
)

func ExampleNew() {
	rdb := redisv9.NewClient(&redisv9.Options{Addr: "localhost:6379"})
	store := idemporedis.New(rdb, idemporedis.WithKeyPrefix("myapp:idempotency"))
	_ = idempo.NewEngine(store)
}

