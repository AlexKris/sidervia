package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTOTPValidation(t *testing.T) {
	secret, err := GenerateTOTPSecret(bytes.NewReader(bytes.Repeat([]byte{3}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	step, ok := ValidateTOTP(secret, code, now)
	if !ok || step != now.Unix()/30 {
		t.Fatalf("step=%d ok=%v", step, ok)
	}
	if _, ok := ValidateTOTP(secret, "0000000", now); ok {
		t.Fatal("accepted invalid code")
	}
	if !strings.HasPrefix(TOTPURI(secret, "admin@example.com"), "otpauth://totp/") {
		t.Fatal("invalid URI")
	}
}
