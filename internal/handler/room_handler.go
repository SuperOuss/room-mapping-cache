package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"room-mapping-cache/internal/redis"

	"github.com/gin-gonic/gin"
	redisc "github.com/redis/go-redis/v9"
)

type RoomHandler struct {
	redisClient *redis.Client
}

func NewRoomHandler(redisClient *redis.Client) *RoomHandler {
	return &RoomHandler{
		redisClient: redisClient,
	}
}

func (h *RoomHandler) GetRoomMappings(c *gin.Context) {
	hotelID := c.Param("hotel_id")
	if hotelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hotel_id is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	redisKey := fmt.Sprintf("room_map:{%s}", hotelID)
	hashData, err := h.redisClient.HGetAll(ctx, redisKey)
	if err != nil {
		if errors.Is(err, redisc.Nil) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room mappings not found for hotel"})
			return
		}
		log.Printf("ERROR: Failed to fetch from Redis hash for key %s: %v", redisKey, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch room mappings"})
		return
	}

	// Check if hash is empty
	if len(hashData) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "room mappings not found for hotel"})
		return
	}

	// Transform hash data to simplified format: only room name (key) and id
	result := make(map[string]int64)
	for roomName, roomValue := range hashData {
		// Parse the JSON value from the hash field
		var roomData map[string]interface{}
		if err := json.Unmarshal([]byte(roomValue), &roomData); err != nil {
			log.Printf("ERROR: Failed to parse room data for %s: %v. Value: %s", roomName, err, roomValue[:min(200, len(roomValue))])
			continue
		}

		// Extract the id field
		var id int64
		if i, ok := roomData["id"].(float64); ok {
			id = int64(i)
		} else {
			continue
		}

		result[roomName] = id
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(result)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal response"})
		return
	}

	// Compress response
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compress response"})
		return
	}
	if err := gz.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close gzip writer"})
		return
	}

	// Set headers and return compressed response
	c.Header("Content-Type", "application/json")
	c.Header("Content-Encoding", "gzip")
	c.Data(http.StatusOK, "application/json", buf.Bytes())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

