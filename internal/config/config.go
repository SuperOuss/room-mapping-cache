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
	var addrs []string
	
	// Support REDIS_HOST and REDIS_PORT (for production)
	redisHost := getEnv("REDIS_HOST", "")
	redisPort := getEnv("REDIS_PORT", "")
	if redisHost != "" && redisPort != "" {
		// Support comma-separated hosts for cluster
		hosts := strings.Split(redisHost, ",")
		for _, host := range hosts {
			host = strings.TrimSpace(host)
			if host != "" {
				addrs = append(addrs, host+":"+redisPort)
			}
		}
	}
	
	// Fallback to REDIS_ADDR if REDIS_HOST/REDIS_PORT not set
	if len(addrs) == 0 {
		redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
		// Support comma-separated addresses for cluster
		addrs = strings.Split(redisAddr, ",")
		for i := range addrs {
			addrs[i] = strings.TrimSpace(addrs[i])
		}
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

