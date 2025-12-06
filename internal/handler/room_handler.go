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
	"regexp"
	"strings"
	"sync"
	"time"

	"room-mapping-cache/internal/redis"

	"github.com/gin-gonic/gin"
	redisc "github.com/redis/go-redis/v9"
)

type RoomHandler struct {
	redisClient *redis.Client
}

type Room struct {
	Name string `json:"name"`
	ID   int64  `json:"id"`
}

type RoomMappingsResponse struct {
	Rooms []Room `json:"rooms"`
}

type BatchRoomMappingsResponse struct {
	Hotels map[string]RoomMappingsResponse `json:"hotels"`
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

	// Use the shared function to fetch room mappings
	result, err := h.fetchRoomMappingsForHotel(ctx, hotelID)
	if err != nil {
		if errors.Is(err, redisc.Nil) {
			c.JSON(http.StatusNotFound, RoomMappingsResponse{Rooms: []Room{}})
			return
		}
		log.Printf("ERROR: Failed to fetch from Redis hash for hotel %s: %v", hotelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch room mappings"})
		return
	}

	// Convert map to array format
	rooms := make([]Room, 0, len(result))
	for name, id := range result {
		rooms = append(rooms, Room{
			Name: name,
			ID:   id,
		})
	}

	response := RoomMappingsResponse{Rooms: rooms}

	// Marshal to JSON
	jsonData, err := json.Marshal(response)
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

// GetRoomMappingsBatch handles batch requests for multiple hotel IDs
func (h *RoomHandler) GetRoomMappingsBatch(c *gin.Context) {
	var request struct {
		HotelIDs []string `json:"hotel_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: hotel_ids array is required"})
		return
	}

	if len(request.HotelIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hotel_ids array cannot be empty"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Fetch room mappings for all hotels in parallel
	type result struct {
		hotelID  string
		mappings map[string]int64
		err      error
	}

	results := make(chan result, len(request.HotelIDs))
	var wg sync.WaitGroup

	for _, hotelID := range request.HotelIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			mappings, err := h.fetchRoomMappingsForHotel(ctx, id)
			results <- result{
				hotelID:  id,
				mappings: mappings,
				err:      err,
			}
		}(hotelID)
	}

	wg.Wait()
	close(results)

	// Collect results
	response := BatchRoomMappingsResponse{
		Hotels: make(map[string]RoomMappingsResponse),
	}
	for res := range results {
		var rooms []Room
		if res.err != nil {
			if errors.Is(res.err, redisc.Nil) {
				// Hotel not found - include empty array
				rooms = []Room{}
			} else {
				log.Printf("ERROR: Failed to fetch room mappings for hotel %s: %v", res.hotelID, res.err)
				// Include empty array for errors too
				rooms = []Room{}
			}
		} else {
			// Convert map to array format
			rooms = make([]Room, 0, len(res.mappings))
			for name, id := range res.mappings {
				rooms = append(rooms, Room{
					Name: name,
					ID:   id,
				})
			}
		}
		response.Hotels[res.hotelID] = RoomMappingsResponse{Rooms: rooms}
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(response)
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

// fetchRoomMappingsForHotel fetches room mappings for a single hotel
func (h *RoomHandler) fetchRoomMappingsForHotel(ctx context.Context, hotelID string) (map[string]int64, error) {
	redisKey := fmt.Sprintf("room_map:{%s}", hotelID)
	hashData, err := h.redisClient.HGetAll(ctx, redisKey)
	if err != nil {
		return nil, err
	}

	// Transform hash data to simplified format: only room name (key) and id
	result := make(map[string]int64)
	for roomName, roomValue := range hashData {
		// Parse the JSON value from the hash field
		var roomData map[string]interface{}
		if err := json.Unmarshal([]byte(roomValue), &roomData); err != nil {
			log.Printf("ERROR: Failed to parse room data for %s in hotel %s: %v", roomName, hotelID, err)
			continue
		}

		// Extract the id field
		var id int64
		if i, ok := roomData["id"].(float64); ok {
			id = int64(i)
		} else {
			continue
		}

		// Normalize room name before adding to result
		normalizedRoomName := normalizeRoomName(roomName)
		result[normalizedRoomName] = id
	}

	return result, nil
}

// normalizeRoomName normalizes room names for consistent comparison
func normalizeRoomName(name string) string {
	// Convert to lowercase and trim spaces
	normalized := strings.ToLower(strings.TrimSpace(name))

	// Replace multiple spaces with a single space
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")

	// Remove common punctuation that doesn't affect meaning
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.ReplaceAll(normalized, ",", " ")
	normalized = strings.ReplaceAll(normalized, ".", " ")
	normalized = strings.ReplaceAll(normalized, "/", " ")
	normalized = strings.ReplaceAll(normalized, "(", " ")
	normalized = strings.ReplaceAll(normalized, ")", " ")

	// Clean up any resulting multiple spaces again
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)

	return normalized
}
