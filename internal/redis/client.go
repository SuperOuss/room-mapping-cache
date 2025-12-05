package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	clusterClient *redis.ClusterClient
}

func NewClient(addrs []string, password string) (*Client, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no Redis addresses provided")
	}

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

// Ping checks if the Redis cluster is accessible
func (c *Client) Ping(ctx context.Context) error {
	return c.clusterClient.Ping(ctx).Err()
}

// HealthCheck performs a more thorough health check by attempting to get cluster info
func (c *Client) HealthCheck(ctx context.Context) error {
	// First, try a simple ping
	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	// Try to get cluster info to verify cluster connectivity
	info, err := c.clusterClient.ClusterInfo(ctx).Result()
	if err != nil {
		return fmt.Errorf("Redis cluster info failed: %w", err)
	}

	// Verify we got a response (basic validation)
	if info == "" {
		return fmt.Errorf("Redis cluster info returned empty response")
	}

	return nil
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.clusterClient.Get(ctx, key).Result()
}

func (c *Client) Close() error {
	return c.clusterClient.Close()
}

