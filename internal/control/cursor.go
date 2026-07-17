package control

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type cursor struct {
	Timestamp int64
	ID        int64
}

func encodeCursor(value cursor) string {
	raw := fmt.Sprintf("v1:%d:%d", value.Timestamp, value.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(value string) (cursor, error) {
	if value == "" {
		return cursor{Timestamp: 1<<63 - 1, ID: 1<<63 - 1}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 128 {
		return cursor{}, errors.New("invalid cursor")
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return cursor{}, errors.New("invalid cursor")
	}
	timestamp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || timestamp < 0 {
		return cursor{}, errors.New("invalid cursor")
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id < 0 {
		return cursor{}, errors.New("invalid cursor")
	}
	return cursor{Timestamp: timestamp, ID: id}, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}
