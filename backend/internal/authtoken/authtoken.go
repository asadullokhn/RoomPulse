// Package authtoken mints and verifies the HS256 JWTs used by both the admin
// panel (role "admin") and the mobile app (role "user").
package authtoken

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// LoadOrCreateSecret resolves the signing secret: the env value wins; else
// the file's contents; else 32 fresh random bytes persisted to filePath so
// restarts don't invalidate every token.
func LoadOrCreateSecret(envValue, filePath string) ([]byte, error) {
	if envValue != "" {
		return []byte(envValue), nil
	}
	if b, err := os.ReadFile(filePath); err == nil && len(b) > 0 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("authtoken: generate secret: %w", err)
	}
	if err := os.WriteFile(filePath, b, 0o600); err != nil {
		return nil, fmt.Errorf("authtoken: persist secret: %w", err)
	}
	return b, nil
}

type Signer struct {
	secret []byte
}

func NewSigner(secret []byte) *Signer { return &Signer{secret: secret} }

func (s *Signer) Mint(sub, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"role": role,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	}).SignedString(s.secret)
}

func (s *Signer) Verify(token string) (sub, role string, err error) {
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return "", "", errors.New("authtoken: invalid claims")
	}
	sub, _ = claims["sub"].(string)
	role, _ = claims["role"].(string)
	if sub == "" || role == "" {
		return "", "", errors.New("authtoken: missing sub/role")
	}
	return sub, role, nil
}
