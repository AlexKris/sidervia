package safelog

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsCommonSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, slog.LevelDebug)
	logger.Error("request Authorization=Bearer-secret", "token", "Bearer abc.def", "error", errors.New("api_key=top-secret"),
		"password", "plain-value-canary", "refresh_token", "opaque-value-canary")
	text := output.String()
	for _, secret := range []string{"Bearer-secret", "abc.def", "top-secret", "plain-value-canary", "opaque-value-canary"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log contains secret %q: %s", secret, text)
		}
	}
}
