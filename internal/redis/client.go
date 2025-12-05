package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	clusterClient *redis.ClusterClient
	client        *redis.Client
	isCluster     bool
}

func NewClient(addrs []string, password string, useCluster bool) (*Client, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no Redis addresses provided")
	}

	if useCluster {
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

		return &Client{clusterClient: rdb, isCluster: true}, nil
	}

	// Single Redis instance mode
	if len(addrs) > 1 {
		return nil, fmt.Errorf("multiple addresses provided but cluster mode is disabled")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         addrs[0],
		Password:     password,
		PoolSize:     100,
		MinIdleConns: 10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	return &Client{client: rdb, isCluster: false}, nil
}

// Ping checks if Redis is accessible
func (c *Client) Ping(ctx context.Context) error {
	if c.isCluster {
		return c.clusterClient.Ping(ctx).Err()
	}
	return c.client.Ping(ctx).Err()
}

// HealthCheck performs a thorough health check
func (c *Client) HealthCheck(ctx context.Context) error {
	// First, try a simple ping
	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	if c.isCluster {
		// Try to get cluster info to verify cluster connectivity
		info, err := c.clusterClient.ClusterInfo(ctx).Result()
		if err != nil {
			return fmt.Errorf("redis cluster info failed: %w", err)
		}

		// Verify we got a response (basic validation)
		if info == "" {
			return fmt.Errorf("redis cluster info returned empty response")
		}
	} else {
		// For single instance, just verify we can get info
		info, err := c.client.Info(ctx, "server").Result()
		if err != nil {
			return fmt.Errorf("redis info failed: %w", err)
		}

		if info == "" {
			return fmt.Errorf("redis info returned empty response")
		}
	}

	return nil
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if c.isCluster {
		return c.clusterClient.Get(ctx, key).Result()
	}
	return c.client.Get(ctx, key).Result()
}

func (c *Client) Close() error {
	if c.isCluster {
		return c.clusterClient.Close()
	}
	return c.client.Close()
}
