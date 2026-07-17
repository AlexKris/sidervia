package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultPasswordParams() PasswordParams {
	p := runtime.NumCPU()
	if p > 4 {
		p = 4
	}
	if p < 1 {
		p = 1
	}
	return PasswordParams{Memory: 64 * 1024, Iterations: 3, Parallelism: uint8(p), SaltLength: 16, KeyLength: 32}
}

type PasswordHasher struct {
	Params PasswordParams
	Random io.Reader
}

func NewPasswordHasher() PasswordHasher {
	return PasswordHasher{Params: DefaultPasswordParams(), Random: rand.Reader}
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	count := utf8.RuneCountInString(password)
	if count < 14 || count > 256 {
		return errors.New("password must contain between 14 and 256 Unicode code points")
	}
	if strings.ContainsRune(password, 0) {
		return errors.New("password must not contain NUL")
	}
	onlySpace := true
	for _, r := range password {
		if !unicode.IsSpace(r) {
			onlySpace = false
			break
		}
	}
	if onlySpace {
		return errors.New("password must not be all whitespace")
	}
	return nil
}

func (h PasswordHasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	p := h.Params
	if p.Memory < 8 || p.Iterations < 1 || p.Parallelism < 1 || p.SaltLength < 8 || p.KeyLength < 16 {
		return "", errors.New("invalid Argon2 parameters")
	}
	salt := make([]byte, p.SaltLength)
	random := h.Random
	if random == nil {
		random = rand.Reader
	}
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func (h PasswordHasher) Verify(password, phc string) (bool, error) {
	params, salt, expected, err := parsePHC(phc)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parsePHC(phc string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, errors.New("invalid Argon2id PHC string")
	}
	var p PasswordParams
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return PasswordParams{}, nil, nil, errors.New("invalid Argon2id parameters")
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return PasswordParams{}, nil, nil, errors.New("invalid Argon2id parameter value")
		}
		switch key {
		case "m":
			p.Memory = uint32(n)
		case "t":
			p.Iterations = uint32(n)
		case "p":
			if n > 255 {
				return PasswordParams{}, nil, nil, errors.New("invalid Argon2id parallelism")
			}
			p.Parallelism = uint8(n)
		default:
			return PasswordParams{}, nil, nil, errors.New("unknown Argon2id parameter")
		}
	}
	if p.Memory < 8 || p.Memory > 1024*1024 || p.Iterations < 1 || p.Iterations > 100 || p.Parallelism < 1 || p.Parallelism > 16 {
		return PasswordParams{}, nil, nil, errors.New("unsafe Argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return PasswordParams{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return PasswordParams{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	return p, salt, expected, nil
}
