package handler

import (
	"context"
	"net/http"
	"time"

	"room-mapping-cache/internal/redis"

	"github.com/gin-gonic/gin"
)

var redisClient *redis.Client

func SetRedisClient(client *redis.Client) {
	redisClient = client
}

func HealthCheck(c *gin.Context) {
	// If Redis client is set, verify Redis connectivity
	if redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := redisClient.HealthCheck(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "Redis cluster is not accessible",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}
