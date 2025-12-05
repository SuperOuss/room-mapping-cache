package config

import (
	"os"
)

type Config struct {
	Addr         string
	Environment  string
	RedisAddr    string
	RedisPassword string
	RedisDB      int
}

func Load() *Config {
	return &Config{
		Addr:         getEnv("ADDR", ":8080"),
		Environment:  getEnv("ENVIRONMENT", "development"),
		RedisAddr:    getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:      0,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

