// Package appleauth verifies Apple "Sign in with Apple" identity tokens.
//
// The mobile app completes the native ASAuthorizationAppleIDProvider flow
// on-device and sends us the resulting identityToken (a JWT signed by
// Apple). Our only job is to verify that JWT — check its signature against
// Apple's published public keys, and check iss/aud/exp — never to exchange
// authorization codes with Apple ourselves. That simpler flow needs no
// Apple client secret and no Apple refresh tokens.
package appleauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appleIssuer         = "https://appleid.apple.com"
	defaultAppleKeysURL = "https://appleid.apple.com/auth/keys"
	keysCacheTTL        = 24 * time.Hour // Apple rotates keys infrequently
)

// Claims is what we trust from a verified Apple identity token.
type Claims struct {
	Sub           string // Apple's stable per-app user identifier
	Email         string // may be a private-relay address; empty if Apple didn't include one
	EmailVerified bool
}

// Verifier verifies Apple identity tokens for one app (identified by bundleID).
type Verifier struct {
	bundleID string
	http     *http.Client

	// KeysURL overrides Apple's real JWKS endpoint — for tests only.
	KeysURL string

	mu       sync.Mutex
	keys     map[string]*rsa.PublicKey // kid -> key
	cachedAt time.Time
}

// NewVerifier builds a Verifier for the given app Bundle ID (checked against
// the token's aud claim). Pass a nil http.Client to use a default with a
// short timeout.
func NewVerifier(bundleID string, hc *http.Client) *Verifier {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{bundleID: bundleID, http: hc, KeysURL: defaultAppleKeysURL}
}

// VerifyIdentityToken verifies the signature and standard claims of an Apple
// identity token, returning the trusted claims on success.
func (v *Verifier) VerifyIdentityToken(ctx context.Context, tokenString string) (Claims, error) {
	token, err := jwt.Parse(tokenString, v.keyFunc(ctx), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return Claims{}, fmt.Errorf("appleauth: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return Claims{}, errors.New("appleauth: invalid token")
	}

	iss, _ := claims["iss"].(string)
	if iss != appleIssuer {
		return Claims{}, fmt.Errorf("appleauth: unexpected issuer %q", iss)
	}
	aud, _ := claims["aud"].(string)
	if v.bundleID == "" || aud != v.bundleID {
		return Claims{}, fmt.Errorf("appleauth: unexpected audience %q", aud)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Claims{}, errors.New("appleauth: missing sub")
	}
	email, _ := claims["email"].(string)
	verified := claims["email_verified"] == "true" || claims["email_verified"] == true

	return Claims{Sub: sub, Email: email, EmailVerified: verified}, nil
}

// keyFunc resolves the RSA public key for the token's kid, fetching/caching
// Apple's JWKS as needed.
func (v *Verifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("appleauth: token missing kid")
		}
		key, err := v.key(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	}
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	stale := time.Since(v.cachedAt) > keysCacheTTL
	key, cached := v.keys[kid]
	v.mu.Unlock()
	if cached && !stale {
		return key, nil
	}

	keys, err := v.fetchKeys(ctx)
	if err != nil {
		if cached {
			return key, nil // fall back to a stale-but-known key rather than fail closed on a transient fetch error
		}
		return nil, fmt.Errorf("appleauth: fetch keys: %w", err)
	}

	v.mu.Lock()
	v.keys = keys
	v.cachedAt = time.Now()
	found, ok := v.keys[kid]
	v.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("appleauth: unknown kid %q", kid)
	}
	return found, nil
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.KeysURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body jwks
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, k := range body.Keys {
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
	}
	return out, nil
}
