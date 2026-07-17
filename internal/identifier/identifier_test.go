package identifier

import (
	"bytes"
	"strings"
	"testing"
)

func TestObjectAndClientKey(t *testing.T) {
	g := Generator{Reader: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128))}
	id, err := g.Object("acct")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "sdr_acct_") {
		t.Fatalf("unexpected id %q", id)
	}
	key, prefix, verifier, err := g.ClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "sk-sdr_"+prefix+"_") || len(verifier) != 64 {
		t.Fatalf("invalid client key result")
	}
}

func TestRejectInvalidKind(t *testing.T) {
	if _, err := NewGenerator().Object("Bad_kind"); err == nil {
		t.Fatal("expected invalid kind error")
	}
}
