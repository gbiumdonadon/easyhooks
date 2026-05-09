package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// MemoryInfo holds the key fields from Redis INFO memory.
type MemoryInfo struct {
	UsedMemory int64  // bytes currently allocated by Redis
	MaxMemory  int64  // configured limit in bytes; 0 = unlimited
	Policy     string // eviction policy (e.g. "volatile-lru", "noeviction")
}

// InfoMemory queries Redis INFO memory and returns the parsed result.
func InfoMemory(ctx context.Context, rdb *goredis.Client) (MemoryInfo, error) {
	raw, err := rdb.Info(ctx, "memory").Result()
	if err != nil {
		return MemoryInfo{}, fmt.Errorf("INFO memory: %w", err)
	}
	var info MemoryInfo
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch k {
		case "used_memory":
			info.UsedMemory, _ = strconv.ParseInt(v, 10, 64)
		case "maxmemory":
			info.MaxMemory, _ = strconv.ParseInt(v, 10, 64)
		case "maxmemory_policy":
			info.Policy = v
		}
	}
	return info, nil
}
