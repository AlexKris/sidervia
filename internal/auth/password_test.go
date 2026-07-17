package auth

import (
	"bytes"
	"testing"
)

func testHasher() PasswordHasher {
	return PasswordHasher{
		Params: PasswordParams{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32},
		Random: bytes.NewReader(bytes.Repeat([]byte{1}, 64)),
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h := testHasher()
	phc, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := h.Verify("correct horse battery staple", phc)
	if err != nil || !ok {
		t.Fatalf("verify=%v err=%v", ok, err)
	}
	ok, err = h.Verify("wrong password long enough", phc)
	if err != nil || ok {
		t.Fatalf("wrong verify=%v err=%v", ok, err)
	}
}

func TestPasswordValidation(t *testing.T) {
	for _, value := range []string{"short", "              ", "valid password\x00but bad"} {
		if err := ValidatePassword(value); err == nil {
			t.Fatalf("expected rejection for %q", value)
		}
	}
}
