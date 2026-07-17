package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const envelopeVersion byte = 1

type Cipher struct {
	aead  cipher.AEAD
	keyID [8]byte
	rand  io.Reader
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(key)
	c := &Cipher{aead: aead, rand: rand.Reader}
	copy(c.keyID[:], sum[:8])
	return c, nil
}

func (c *Cipher) KeyID() string { return hex.EncodeToString(c.keyID[:]) }

func (c *Cipher) Seal(plaintext, aad []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("refusing to encrypt empty plaintext")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	result := make([]byte, 0, 1+len(c.keyID)+len(nonce)+len(plaintext)+c.aead.Overhead())
	result = append(result, envelopeVersion)
	result = append(result, c.keyID[:]...)
	result = append(result, nonce...)
	result = c.aead.Seal(result, nonce, plaintext, aad)
	return result, nil
}

func (c *Cipher) Open(envelope, aad []byte) ([]byte, error) {
	header := 1 + len(c.keyID) + c.aead.NonceSize()
	if len(envelope) < header+c.aead.Overhead() || envelope[0] != envelopeVersion {
		return nil, errors.New("invalid encrypted envelope")
	}
	if subtle.ConstantTimeCompare(envelope[1:9], c.keyID[:]) != 1 {
		return nil, errors.New("encrypted envelope uses a different master key")
	}
	nonce := envelope[9:header]
	plain, err := c.aead.Open(nil, nonce, envelope[header:], aad)
	if err != nil {
		return nil, errors.New("encrypted envelope authentication failed")
	}
	return plain, nil
}

func AAD(table, publicID, column string) []byte {
	return []byte("sidervia:v1:" + table + ":" + publicID + ":" + column)
}
