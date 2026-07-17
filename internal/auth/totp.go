package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod uint = 30

func GenerateTOTPSecret(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	value := make([]byte, 20)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value), nil
}

func TOTPURI(secret, account string) string {
	label := "Sidervia:" + account
	values := url.Values{
		"secret":    []string{secret},
		"issuer":    []string{"Sidervia"},
		"period":    []string{strconv.Itoa(int(totpPeriod))},
		"digits":    []string{"6"},
		"algorithm": []string{"SHA1"},
	}
	return "otpauth://totp/" + url.PathEscape(label) + "?" + values.Encode()
}

func ValidateTOTP(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, offset := range []int64{0, -1, 1} {
		step := now.Unix()/int64(totpPeriod) + offset
		candidateTime := time.Unix(step*int64(totpPeriod), 0)
		candidate, err := totp.GenerateCodeCustom(secret, candidateTime, totp.ValidateOpts{
			Period:    totpPeriod,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(strings.TrimSpace(code))) == 1 {
			return step, true
		}
	}
	return 0, false
}

func TOTPCode(secret string, at time.Time) (string, error) {
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", fmt.Errorf("generate TOTP code: %w", err)
	}
	return code, nil
}
