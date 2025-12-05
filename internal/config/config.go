package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr          string
	Environment   string
	RedisAddrs    []string
	RedisPassword string
}

func Load() *Config {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	// Support comma-separated addresses for cluster
	addrs := strings.Split(redisAddr, ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
	}

	return &Config{
		Addr:          getEnv("ADDR", ":8080"),
		Environment:   getEnv("ENVIRONMENT", "development"),
		RedisAddrs:    addrs,
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

