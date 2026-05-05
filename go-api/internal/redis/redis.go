package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
)

func NewClient(cfg *config.Config) (*goredis.Client, error) {
	opt, err := goredis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	opt.PoolSize = cfg.RedisPoolSize
	client := goredis.NewClient(opt)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
