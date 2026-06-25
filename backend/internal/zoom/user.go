package zoom

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuthorizeURL = "https://zoom.us/oauth/authorize"
)

// ErrNotAuthorized means no user OAuth token is stored yet (visit /oauth/login).
var ErrNotAuthorized = errors.New("zoom: not authorized — complete the OAuth login flow")

// UserConfig carries user-managed OAuth app settings.
type UserConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	TokenFile    string // where the access/refresh token is persisted
	APIBase      string
	TokenURL     string
	AuthorizeURL string
}

// oauthToken is the persisted token set.
type oauthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// UserClient implements Client via the OAuth 2.0 authorization-code flow,
// acting as the logged-in user. Reads the user's own reservations; check-in/out
// and workspace-list typically require admin scopes and will error.
type UserClient struct {
	cfg  UserConfig
	http *http.Client
	log  *slog.Logger

	mu    sync.Mutex
	tok   *oauthToken
	state string
}

// NewUserClient wires the user client and loads any persisted token.
func NewUserClient(cfg UserConfig, hc *http.Client, log *slog.Logger) *UserClient {
	if cfg.APIBase == "" {
		cfg.APIBase = defaultAPIBase
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultTokenURL
	}
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = defaultAuthorizeURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	c := &UserClient{cfg: cfg, http: hc, log: log}
	if t, err := loadToken(cfg.TokenFile); err == nil {
		c.tok = t
		log.Info("loaded persisted zoom user token", "expires", t.Expiry.Format(time.RFC3339))
	}
	return c
}

// --- OAuth flow ------------------------------------------------------------

// AuthCodeURL builds the authorize URL and remembers the CSRF state.
func (c *UserClient) AuthCodeURL() string {
	c.mu.Lock()
	c.state = randomState()
	state := c.state
	c.mu.Unlock()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.cfg.ClientID)
	q.Set("redirect_uri", c.cfg.RedirectURI)
	q.Set("state", state)
	return c.cfg.AuthorizeURL + "?" + q.Encode()
}

// Exchange swaps an authorization code for tokens and persists them. It
// validates state to guard against CSRF.
func (c *UserClient) Exchange(ctx context.Context, code, state string) error {
	c.mu.Lock()
	expected := c.state
	c.mu.Unlock()
	if expected == "" || state != expected {
		return errors.New("zoom: oauth state mismatch")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.cfg.RedirectURI)

	return c.tokenRequest(ctx, form)
}

func (c *UserClient) refresh(ctx context.Context) error {
	c.mu.Lock()
	rt := ""
	if c.tok != nil {
		rt = c.tok.RefreshToken
	}
	c.mu.Unlock()
	if rt == "" {
		return ErrNotAuthorized
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	return c.tokenRequest(ctx, form)
}

// tokenRequest posts to the token endpoint with Basic auth and stores the result.
func (c *UserClient) tokenRequest(ctx context.Context, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build token request: %w", err)
	}
	basic := base64.StdEncoding.EncodeToString([]byte(c.cfg.ClientID + ":" + c.cfg.ClientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}

	tok := &oauthToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}
	c.mu.Lock()
	c.tok = tok
	c.mu.Unlock()

	if err := saveToken(c.cfg.TokenFile, tok); err != nil {
		c.log.Warn("could not persist token", "err", err)
	}
	c.log.Info("zoom user token stored", "expires_in_s", tr.ExpiresIn)
	return nil
}

// token returns a valid access token, refreshing if needed.
func (c *UserClient) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok := c.tok
	c.mu.Unlock()
	if tok == nil {
		return "", ErrNotAuthorized
	}
	if time.Now().Before(tok.Expiry.Add(-tokenExpiryGuard)) {
		return tok.AccessToken, nil
	}
	if err := c.refresh(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok.AccessToken, nil
}

// Authorized reports whether a token is present.
func (c *UserClient) Authorized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok != nil
}

// --- Client interface impl -------------------------------------------------

func (c *UserClient) doGET(ctx context.Context, path string, query url.Values, out any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	u := c.cfg.APIBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("GET %s: forbidden (scope not granted): %s", path, strings.TrimSpace(string(raw)))
	case resp.StatusCode >= 300:
		return fmt.Errorf("GET %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ListWorkspaces tries the admin workspace-list endpoint; user scopes usually
// can't reach it, so a forbidden result is downgraded to "no rooms" and the
// sync derives rooms from reservations instead.
func (c *UserClient) ListWorkspaces(ctx context.Context, locationID string) ([]Workspace, error) {
	q := url.Values{}
	if locationID != "" {
		q.Set("location_id", locationID)
	}
	var resp wsListResponse
	if err := c.doGET(ctx, "/workspaces", q, &resp); err != nil {
		c.log.Warn("workspace list unavailable in user mode; deriving rooms from reservations", "err", err)
		return nil, nil
	}
	out := make([]Workspace, 0, len(resp.Workspaces))
	for _, w := range resp.Workspaces {
		out = append(out, Workspace{
			ID: w.ID, Name: w.Name, Type: w.Type, LocationID: w.LocationID,
			Capacity: w.Capacity, Status: w.Status, HasTV: strings.EqualFold(w.Type, "ZoomRoom"),
		})
	}
	return out, nil
}

func (c *UserClient) ListReservations(ctx context.Context, _ string, from, to time.Time) ([]Reservation, error) {
	q := url.Values{}
	q.Set("from", from.Format(time.RFC3339))
	q.Set("to", to.Format(time.RFC3339))

	var resp userResListResponse
	if err := c.doGET(ctx, "/workspaces/users/me/reservations", q, &resp); err != nil {
		return nil, err
	}
	out := make([]Reservation, 0, len(resp.Reservations))
	for _, r := range resp.Reservations {
		start, _ := time.Parse(time.RFC3339, r.StartTime)
		end, _ := time.Parse(time.RFC3339, r.EndTime)
		out = append(out, Reservation{
			ReservationID: r.ReservationID, WorkspaceID: r.WorkspaceID,
			WorkspaceName: r.WorkspaceName, LocationName: r.LocationName,
			UserID: r.UserID, UserEmail: r.UserEmail,
			StartTime: start, EndTime: end, CheckInStatus: r.CheckInStatus,
		})
	}
	return out, nil
}

// SendEvent is unsupported in user mode (check-in/out needs admin write scope).
func (c *UserClient) SendEvent(_ context.Context, _ EventType, _ string) error {
	return errors.New("zoom: check-in/out requires admin scope (workspace:write:admin); not available in user mode")
}

type userResListResponse struct {
	Reservations []struct {
		ReservationID string `json:"reservation_id"`
		WorkspaceID   string `json:"workspace_id"`
		WorkspaceName string `json:"workspace_name"`
		LocationName  string `json:"location_name"`
		UserID        string `json:"user_id"`
		UserEmail     string `json:"user_email"`
		StartTime     string `json:"start_time"`
		EndTime       string `json:"end_time"`
		CheckInStatus string `json:"check_in_status"`
	} `json:"reservations"`
}

// --- token persistence -----------------------------------------------------

func loadToken(path string) (*oauthToken, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t oauthToken
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("empty token file")
	}
	return &t, nil
}

func saveToken(path string, t *oauthToken) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func randomState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("roompulse-state"))
	}
	return hex.EncodeToString(b)
}
