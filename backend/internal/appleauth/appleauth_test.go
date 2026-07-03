package appleauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testJWKS spins up a fake Apple keys endpoint backed by a freshly generated
// RSA key, and returns a signer function for building test identity tokens.
func testJWKS(t *testing.T) (*httptest.Server, func(claims jwt.MapClaims) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-kid-1"

	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(bigIntToBytes(key.PublicKey.E)),
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	sign := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return s
	}
	return srv, sign
}

func bigIntToBytes(e int) []byte {
	// Minimal big-endian encoding of a small int (Apple's "e" is always 65537 = 0x010001).
	if e == 65537 {
		return []byte{0x01, 0x00, 0x01}
	}
	return []byte{byte(e)}
}

func TestVerifyIdentityToken_Valid(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"email": "a@example.com", "email_verified": "true",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	claims, err := v.VerifyIdentityToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyIdentityToken: %v", err)
	}
	if claims.Sub != "apple-sub-123" || claims.Email != "a@example.com" || !claims.EmailVerified {
		t.Fatalf("claims = %+v, want sub=apple-sub-123 email=a@example.com verified=true", claims)
	}
}

func TestVerifyIdentityToken_WrongAudience(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.other.App", "sub": "apple-sub-123",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with wrong audience: want error, got nil")
	}
}

func TestVerifyIdentityToken_Expired(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": appleIssuer, "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with expired token: want error, got nil")
	}
}

func TestVerifyIdentityToken_WrongIssuer(t *testing.T) {
	srv, sign := testJWKS(t)
	v := NewVerifier("com.example.QuickRoom", nil)
	v.KeysURL = srv.URL

	now := time.Now()
	token := sign(jwt.MapClaims{
		"iss": "https://evil.example.com", "aud": "com.example.QuickRoom", "sub": "apple-sub-123",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})

	if _, err := v.VerifyIdentityToken(context.Background(), token); err == nil {
		t.Fatal("VerifyIdentityToken with wrong issuer: want error, got nil")
	}
}
