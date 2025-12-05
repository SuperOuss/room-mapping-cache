package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"room-mapping-cache/internal/config"
	"room-mapping-cache/internal/handler"
	"room-mapping-cache/internal/redis"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	redisMode := "single instance"
	if cfg.UseCluster {
		redisMode = "cluster"
	}
	log.Printf("Initializing Redis %s client with addresses: %v", redisMode, cfg.RedisAddrs)

	// Initialize Redis client (cluster or single instance based on config)
	redisClient, err := redis.NewClient(cfg.RedisAddrs, cfg.RedisPassword, cfg.UseCluster)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	// Perform thorough Redis connection check on startup
	log.Printf("Checking Redis %s connectivity...", redisMode)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := redisClient.HealthCheck(ctx); err != nil {
		log.Fatalf("CRITICAL: Failed to connect to Redis %s: %v. Service will not start.", redisMode, err)
	}
	log.Printf("Redis %s connection verified successfully", redisMode)

	// Start background health check goroutine that will crash the service if Redis becomes unavailable
	go monitorRedisHealth(redisClient)

	// Set up router
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Initialize handler
	roomHandler := handler.NewRoomHandler(redisClient)
	handler.SetRedisClient(redisClient)

	// Routes
	router.GET("/health", handler.HealthCheck)
	router.GET("/room-mappings/:hotel_id", roomHandler.GetRoomMappings)

	// Start server
	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	log.Printf("Server started on %s", cfg.Addr)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

// monitorRedisHealth periodically checks Redis connectivity and crashes the service if it fails
func monitorRedisHealth(redisClient *redis.Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := redisClient.HealthCheck(ctx)
		cancel()

		if err != nil {
			log.Fatalf("CRITICAL: Redis health check failed: %v. Service is crashing.", err)
		}
		log.Println("Redis health check passed")
	}
}

