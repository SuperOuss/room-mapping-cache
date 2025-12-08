package handler

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"room-mapping-cache/internal/redis"

	"github.com/gin-gonic/gin"
	redisc "github.com/redis/go-redis/v9"
)

var (
	wsRe          = regexp.MustCompile(`\s+`)
	punctReplacer = strings.NewReplacer(
		"-", " ",
		",", " ",
		".", " ",
		"/", " ",
		"(", " ",
		")", " ",
	)

	gzipPool = sync.Pool{
		New: func() any {
			// BestSpeed is usually the right tradeoff for 1000 rps services.
			w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
			return w
		},
	}
)

type RoomHandler struct {
	redisClient *redis.Client
}

type Room struct {
	Name string `json:"name"`
	ID   int64  `json:"id"`
}

type roomValue struct {
	ID json.Number `json:"id"`
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

	// Use the shared function to fetch room mappings (tries both hashtagged and non-hashtagged)
	rooms, err := h.fetchRoomsForHotel(ctx, hotelID)
	if err != nil {
		log.Printf("ERROR: Failed to fetch from Redis hash for hotel %s: %v", hotelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch room mappings"})
		return
	}

	writeJSONMaybeGzip(c, RoomMappingsResponse{Rooms: rooms})
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

	// Hard caps are essential at 1000 rps
	if len(request.HotelIDs) == 0 || len(request.HotelIDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hotel_ids must contain 1..100 items"})
		return
	}

	// Dedup to avoid duplicate Redis work (common in callers)
	hotelIDs := dedupStringsInPlace(request.HotelIDs)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 1500*time.Millisecond)
	defer cancel()

	// -------- Redis pipelining (no goroutines) --------
	// Try primary keys first (as provided), then fallback keys
	pipe := h.redisClient.Pipeline()
	primaryCmds := make([]*redisc.MapStringStringCmd, 0, len(hotelIDs))
	fallbackCmds := make([]*redisc.MapStringStringCmd, 0, len(hotelIDs))
	keys := make([]string, 0, len(hotelIDs))

	for _, hotelID := range hotelIDs {
		// Primary key: try with original hotel ID
		primaryKey := fmt.Sprintf("room_map:{%s}", hotelID)
		keys = append(keys, hotelID)
		primaryCmds = append(primaryCmds, pipe.HGetAll(ctx, primaryKey))

		// Fallback key: try alternate version (with # if original didn't have it, without # if it did)
		fallbackID := getAlternateHotelID(hotelID)
		fallbackKey := fmt.Sprintf("room_map:{%s}", fallbackID)
		fallbackCmds = append(fallbackCmds, pipe.HGetAll(ctx, fallbackKey))
	}

	_, execErr := pipe.Exec(ctx)
	// Exec can return a non-nil error even when some commands succeeded.
	// We'll treat per-hotel errors individually below via cmd.Err().
	if execErr != nil && !errors.Is(execErr, redisc.Nil) {
		log.Printf("ERROR: redis pipeline exec failed: %v", execErr)
		// still continue, cmds may contain partial results
	}

	// -------- Build response --------
	response := BatchRoomMappingsResponse{
		Hotels: make(map[string]RoomMappingsResponse, len(hotelIDs)),
	}

	for i := range hotelIDs {
		hotelID := keys[i]
		primaryCmd := primaryCmds[i]
		fallbackCmd := fallbackCmds[i]

		// Try primary key first
		hashData, err := primaryCmd.Result()
		if err != nil || len(hashData) == 0 {
			// If primary failed or empty, try fallback
			hashData, err = fallbackCmd.Result()
			if err != nil || len(hashData) == 0 {
				// Both failed -> empty
				response.Hotels[hotelID] = RoomMappingsResponse{Rooms: []Room{}}
				continue
			}
		}

		rooms := parseRooms(hashData)
		response.Hotels[hotelID] = RoomMappingsResponse{Rooms: rooms}
	}

	writeJSONMaybeGzip(c, response)
}

// fetchRoomsForHotel fetches room mappings for a single hotel
// Tries both hashtagged and non-hashtagged versions
func (h *RoomHandler) fetchRoomsForHotel(ctx context.Context, hotelID string) ([]Room, error) {
	// Try primary key first (as provided)
	primaryKey := fmt.Sprintf("room_map:{%s}", hotelID)
	hashData, err := h.redisClient.HGetAll(ctx, primaryKey)
	if err == nil && len(hashData) > 0 {
		return parseRooms(hashData), nil
	}

	// If primary failed or empty, try alternate version
	fallbackID := getAlternateHotelID(hotelID)
	fallbackKey := fmt.Sprintf("room_map:{%s}", fallbackID)
	hashData, err = h.redisClient.HGetAll(ctx, fallbackKey)
	if err != nil {
		return nil, err
	}
	return parseRooms(hashData), nil
}

// getAlternateHotelID returns the alternate version of a hotel ID
// If it has # prefix, returns without it; if it doesn't, returns with it
func getAlternateHotelID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "#") {
		return strings.TrimPrefix(id, "#")
	}
	return "#" + id
}

// normalizeRoomName normalizes room names for consistent comparison
func normalizeRoomName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = punctReplacer.Replace(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func parseRooms(hashData map[string]string) []Room {
	// Guardrail: cap processed rooms to avoid CPU/memory explosion on huge hashes
	const maxRoomsToProcess = 2000
	if len(hashData) > maxRoomsToProcess {
		log.Printf("WARNING: hotel has %d rooms, truncating processing to %d", len(hashData), maxRoomsToProcess)
	}

	rooms := make([]Room, 0, len(hashData))
	count := 0

	for roomName, roomJSON := range hashData {
		if count >= maxRoomsToProcess {
			break
		}

		var rv roomValue
		// Optimization: could use byte scanning for "id" to avoid allocations,
		// but Unmarshal is safe and pipeline provides biggest win.
		if err := json.Unmarshal([]byte(roomJSON), &rv); err != nil {
			log.Printf("ERROR: Failed to parse room data: %v", err)
			continue
		}

		id, err := rv.ID.Int64()
		if err != nil || id == 0 {
			continue
		}

		rooms = append(rooms, Room{
			Name: normalizeRoomName(roomName),
			ID:   id,
		})
		count++
	}

	// Stable order for clients & caching
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Name < rooms[j].Name })

	return rooms
}

func writeJSONMaybeGzip(c *gin.Context, v any) {
	c.Header("Content-Type", "application/json")

	ae := c.GetHeader("Accept-Encoding")
	if strings.Contains(ae, "gzip") {
		c.Header("Content-Encoding", "gzip")
		w := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(w)

		w.Reset(c.Writer)
		defer w.Close()

		enc := json.NewEncoder(w)
		_ = enc.Encode(v)
		return
	}

	enc := json.NewEncoder(c.Writer)
	_ = enc.Encode(v)
}

func dedupStringsInPlace(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
