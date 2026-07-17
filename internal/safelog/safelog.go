package safelog

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)sk-sdr_[a-z0-9_-]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|session|cookie|authorization)=)[^&\s]+`),
	regexp.MustCompile(`(?i)("(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|session|cookie|authorization)"\s*:\s*")[^"]+`),
}

func New(output io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if sensitiveAttributeKey(attribute.Key) {
				attribute.Value = slog.StringValue("[REDACTED]")
				return attribute
			}
			switch attribute.Value.Kind() {
			case slog.KindString:
				attribute.Value = slog.StringValue(Redact(attribute.Value.String()))
			case slog.KindAny:
				if err, ok := attribute.Value.Any().(error); ok {
					attribute.Value = slog.StringValue(Redact(err.Error()))
				}
			}
			return attribute
		},
	})
	return slog.New(&contextRedactingHandler{next: handler})
}

func sensitiveAttributeKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch key {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "api_key", "client_key",
		"token", "access_token", "refresh_token", "password", "credential", "secret", "session",
		"session_token", "csrf", "csrf_token", "totp_secret", "oauth_code":
		return true
	default:
		return false
	}
}

func Redact(value string) string {
	result := value
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, redactMatch)
	}
	return result
}

func redactMatch(value string) string {
	if index := strings.IndexAny(value, "=: "); index >= 0 {
		return value[:index+1] + "[REDACTED]"
	}
	return "[REDACTED]"
}

type contextRedactingHandler struct{ next slog.Handler }

func (h *contextRedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextRedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	record.Message = Redact(record.Message)
	return h.next.Handle(ctx, record)
}

func (h *contextRedactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &contextRedactingHandler{next: h.next.WithAttrs(attributes)}
}

func (h *contextRedactingHandler) WithGroup(name string) slog.Handler {
	return &contextRedactingHandler{next: h.next.WithGroup(name)}
}
