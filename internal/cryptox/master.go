package cryptox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/AlexKris/sidervia/internal/securefile"
)

const KeySize = 32

func LoadMasterKey(path string) ([]byte, error) {
	raw, err := securefile.Read(path, 1024)
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("master key file must contain standard base64: %w", err)
	}
	if len(decoded) != KeySize {
		return nil, fmt.Errorf("master key must decode to %d bytes", KeySize)
	}
	return decoded, nil
}

func Derive(key []byte, label string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("sidervia:v1:" + label))
	return h.Sum(nil)
}
