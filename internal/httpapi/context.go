package httpapi

import (
	"context"

	"github.com/AlexKris/sidervia/internal/auth"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	sessionKey   contextKey = "session"
)

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func sessionFrom(ctx context.Context) (auth.Session, bool) {
	value, ok := ctx.Value(sessionKey).(auth.Session)
	return value, ok
}
