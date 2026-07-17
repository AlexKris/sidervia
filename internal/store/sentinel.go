package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/AlexKris/sidervia/internal/cryptox"
)

var sentinelPlaintext = []byte("sidervia-crypto-sentinel-v1")

func (s *Store) VerifyOrCreateSentinel(ctx context.Context, cipher *cryptox.Cipher) error {
	return s.verifySentinel(ctx, cipher, true)
}

func (s *Store) VerifySentinel(ctx context.Context, cipher *cryptox.Cipher) error {
	return s.verifySentinel(ctx, cipher, false)
}

func (s *Store) verifySentinel(ctx context.Context, cipher *cryptox.Cipher, allowCreate bool) error {
	var keyID string
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx, "SELECT key_id, ciphertext FROM crypto_sentinel WHERE id = 1").Scan(&keyID, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		if !allowCreate {
			return errors.New("crypto sentinel is missing")
		}
		sealed, err := cipher.Seal(sentinelPlaintext, cryptox.AAD("crypto_sentinel", "1", "ciphertext"))
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, "INSERT INTO crypto_sentinel(id, key_id, ciphertext) VALUES(1, ?, ?)", cipher.KeyID(), sealed)
		if err != nil {
			return fmt.Errorf("create crypto sentinel: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read crypto sentinel: %w", err)
	}
	if keyID != cipher.KeyID() {
		return errors.New("master key does not match database")
	}
	plain, err := cipher.Open(ciphertext, cryptox.AAD("crypto_sentinel", "1", "ciphertext"))
	if err != nil {
		return fmt.Errorf("verify crypto sentinel: %w", err)
	}
	if string(plain) != string(sentinelPlaintext) {
		return errors.New("crypto sentinel plaintext is invalid")
	}
	return nil
}
