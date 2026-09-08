package ordertoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

const byteLength = 32

var ErrInvalid = errors.New("invalid order token")

func Generate() (string, string, error) {
	rawBytes := make([]byte, byteLength)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(rawBytes)
	hash := sha256.Sum256(rawBytes)
	return raw, hex.EncodeToString(hash[:]), nil
}

func Hash(raw string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != byteLength {
		return "", ErrInvalid
	}
	hash := sha256.Sum256(decoded)
	return hex.EncodeToString(hash[:]), nil
}

func Derive(secret, customerRef, idempotencyKey string) (string, string, error) {
	if len(secret) < 32 {
		return "", "", ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(customerRef))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(idempotencyKey))
	rawBytes := mac.Sum(nil)
	raw := base64.RawURLEncoding.EncodeToString(rawBytes)
	hash := sha256.Sum256(rawBytes)
	return raw, hex.EncodeToString(hash[:]), nil
}
