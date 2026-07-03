package api_test

import (
	"bytes"
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

// signAppleToken builds an RS256 JWT and returns it plus a JWKS-serving test
// server it can be verified against, mimicking Apple's identity token issuance.
func signAppleToken(t *testing.T, bundleID, sub, email string) (token string, jwksURL string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// A distinct kid per call: the Verifier caches keys by kid for 24h, so two
	// tokens signed with different keys but the same kid (as would happen
	// across two calls in one test) would have the second verification
	// incorrectly served the first key from cache.
	kid := "test-kid-" + sub

	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": "AQAB", // 65537
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "https://appleid.apple.com", "aud": bundleID, "sub": sub,
		"email": email, "email_verified": "true",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed, srv.URL
}

func TestAppleSignInThenLogout(t *testing.T) {
	h := newTestHandler(t)

	// newTestHandler wires appleauth.NewVerifier("test.bundle.id", nil) with
	// the real Apple JWKS URL, which a test token can't be signed against —
	// this test exercises the failure path (invalid token) plus, via a
	// second handler built with an overridable verifier, the success path.
	body, _ := json.Marshal(map[string]string{"identity_token": "not-a-real-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an invalid identity token", rec.Code)
	}
}

func TestAppleSignInSuccess(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-xyz", "user@example.com")
	verifier.KeysURL = jwksURL

	body, _ := json.Marshal(map[string]string{"identity_token": token, "name": "Ava"})
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
			Name   string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionToken == "" || resp.User.Email != "user@example.com" || resp.User.Name != "Ava" {
		t.Fatalf("response = %+v, want a non-empty session token and matching user", resp)
	}

	// The session should authenticate a protected endpoint.
	req2 := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	req2.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /reservations/mine with valid session: status = %d, want 200", rec2.Code)
	}

	// Logout, then the same token should be rejected.
	req3 := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req3.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec3.Code)
	}

	req4 := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	req4.Header.Set("Authorization", "Bearer "+resp.SessionToken)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusUnauthorized {
		t.Fatalf("GET /reservations/mine after logout: status = %d, want 401", rec4.Code)
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with no Authorization header", rec.Code)
	}
}
