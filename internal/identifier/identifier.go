package identifier

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

var encoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

type Generator struct {
	Reader io.Reader
}

func NewGenerator() Generator { return Generator{Reader: rand.Reader} }

func (g Generator) Object(kind string) (string, error) {
	if !validKind(kind) {
		return "", errors.New("invalid object kind")
	}
	b := make([]byte, 16)
	if _, err := io.ReadFull(g.reader(), b); err != nil {
		return "", err
	}
	return "sdr_" + kind + "_" + strings.ToLower(encoding.EncodeToString(b)), nil
}

func (g Generator) Token(bytes int) (string, error) {
	if bytes < 16 {
		return "", errors.New("token size is too small")
	}
	b := make([]byte, bytes)
	if _, err := io.ReadFull(g.reader(), b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (g Generator) ClientKey() (full, prefix, verifier string, err error) {
	prefix, err = g.randomEncoded(6)
	if err != nil {
		return "", "", "", err
	}
	prefix = prefix[:8]
	secret, err := g.Token(32)
	if err != nil {
		return "", "", "", err
	}
	full = "sk-sdr_" + prefix + "_" + secret
	verifier = Verifier(secret)
	return full, prefix, verifier, nil
}

func (g Generator) randomEncoded(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := io.ReadFull(g.reader(), b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Verifier(value string) string {
	return SHA256Hex([]byte(value))
}

func (g Generator) reader() io.Reader {
	if g.Reader == nil {
		return rand.Reader
	}
	return g.Reader
}

func validKind(kind string) bool {
	if kind == "" || len(kind) > 12 {
		return false
	}
	for _, r := range kind {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
