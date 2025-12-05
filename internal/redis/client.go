package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	clusterClient *redis.ClusterClient
}

func NewClient(addrs []string, password string) (*Client, error) {
	rdb := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        addrs,
		Password:     password,
		PoolSize:     100,
		MinIdleConns: 10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		MaxRetries:   3,
	})

	return &Client{clusterClient: rdb}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.clusterClient.Ping(ctx).Err()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.clusterClient.Get(ctx, key).Result()
}

func (c *Client) Close() error {
	return c.clusterClient.Close()
}

