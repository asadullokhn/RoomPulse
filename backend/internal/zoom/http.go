package zoom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIBase   = "https://api.zoom.us/v2"
	defaultTokenURL  = "https://zoom.us/oauth/token"
	tokenExpiryGuard = 60 * time.Second // refresh a minute early
)

// HTTPConfig carries the Server-to-Server OAuth credentials.
type HTTPConfig struct {
	AccountID    string
	ClientID     string
	ClientSecret string
	APIBase      string // optional override; defaults to api.zoom.us/v2
	TokenURL     string // optional override
}

// HTTPClient is the live Zoom client. It manages the S2S OAuth token
// (cache + refresh) and talks to the Workspace Reservation endpoints.
type HTTPClient struct {
	cfg  HTTPConfig
	http *http.Client
	log  *slog.Logger

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewHTTPClient wires the live client. http may be nil (a sane default is used).
func NewHTTPClient(cfg HTTPConfig, hc *http.Client, log *slog.Logger) *HTTPClient {
	if cfg.APIBase == "" {
		cfg.APIBase = defaultAPIBase
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultTokenURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPClient{cfg: cfg, http: hc, log: log}
}

// --- OAuth -----------------------------------------------------------------

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// token returns a valid bearer token, refreshing if expired.
func (c *HTTPClient) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-tokenExpiryGuard)) {
		return c.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "account_credentials")
	form.Set("account_id", c.cfg.AccountID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	basic := base64.StdEncoding.EncodeToString([]byte(c.cfg.ClientID + ":" + c.cfg.ClientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	c.accessToken = tr.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	c.log.Info("obtained zoom access token", "expires_in_s", tr.ExpiresIn)
	return c.accessToken, nil
}

// doJSON performs an authenticated request and decodes a JSON response into out.
func (c *HTTPClient) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}

	u := c.cfg.APIBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reader = strings.NewReader(string(b))
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return ErrReservationNotFound
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// --- Endpoint DTOs (Zoom wire shape) ---------------------------------------

type wsListResponse struct {
	Workspaces []struct {
		ID         string `json:"id"`
		Name       string `json:"workspace_name"`
		Type       string `json:"workspace_type"`
		LocationID string `json:"location_id"`
		Capacity   int    `json:"capacity"`
		Status     string `json:"workspace_status"`
	} `json:"workspaces"`
	NextPageToken string `json:"next_page_token"`
}

type resListResponse struct {
	Reservations []struct {
		ReservationID string `json:"reservation_id"`
		WorkspaceID   string `json:"workspace_id"`
		UserID        string `json:"user_id"`
		UserEmail     string `json:"user_email"`
		StartTime     string `json:"start_time"`
		EndTime       string `json:"end_time"`
		CheckInStatus string `json:"check_in_status"`
	} `json:"reservations"`
	NextPageToken string `json:"next_page_token"`
}

// --- Client interface impl -------------------------------------------------

func (c *HTTPClient) ListWorkspaces(ctx context.Context, locationID string) ([]Workspace, error) {
	q := url.Values{}
	if locationID != "" {
		q.Set("location_id", locationID)
	}
	var resp wsListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/workspaces", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(resp.Workspaces))
	for _, w := range resp.Workspaces {
		out = append(out, Workspace{
			ID: w.ID, Name: w.Name, Type: w.Type, LocationID: w.LocationID,
			Capacity: w.Capacity, Status: w.Status,
			HasTV: strings.EqualFold(w.Type, "ZoomRoom"),
		})
	}
	return out, nil
}

func (c *HTTPClient) ListReservations(ctx context.Context, locationID string, from, to time.Time) ([]Reservation, error) {
	q := url.Values{}
	if locationID != "" {
		q.Set("location_id", locationID)
	}
	q.Set("from", from.Format(time.RFC3339))
	q.Set("to", to.Format(time.RFC3339))

	var resp resListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/workspaces/reservations", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Reservation, 0, len(resp.Reservations))
	for _, r := range resp.Reservations {
		start, _ := time.Parse(time.RFC3339, r.StartTime)
		end, _ := time.Parse(time.RFC3339, r.EndTime)
		out = append(out, Reservation{
			ReservationID: r.ReservationID, WorkspaceID: r.WorkspaceID,
			UserID: r.UserID, UserEmail: r.UserEmail,
			StartTime: start, EndTime: end, CheckInStatus: r.CheckInStatus,
		})
	}
	return out, nil
}

func (c *HTTPClient) SendEvent(ctx context.Context, event EventType, reservationID string) error {
	body := map[string]string{
		"event":          string(event),
		"reservation_id": reservationID,
	}
	return c.doJSON(ctx, http.MethodPost, "/workspaces/events", nil, body, nil)
}
