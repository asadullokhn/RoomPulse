package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListUsersAndUserReservations(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-list-1", "lister@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token, "name": "Lister"})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	listReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /users status = %d, want 200", listRec.Code)
	}
	var listResp struct {
		Users []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
		} `json:"users"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	found := false
	for _, u := range listResp.Users {
		if u.UserID == authResp.User.UserID && u.Email == "lister@example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("GET /users = %s, want to find user %s", listRec.Body.String(), authResp.User.UserID)
	}

	start := time.Now().Add(96 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-mengwi", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create reservation status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	resReq := httptest.NewRequest(http.MethodGet, "/users/"+authResp.User.UserID+"/reservations", nil)
	resReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	resRec := httptest.NewRecorder()
	h.ServeHTTP(resRec, resReq)
	if resRec.Code != http.StatusOK {
		t.Fatalf("GET /users/{id}/reservations status = %d, want 200", resRec.Code)
	}
	var resResp struct {
		Reservations []struct {
			WorkspaceID string `json:"zoom_workspace_id"`
		} `json:"reservations"`
	}
	_ = json.Unmarshal(resRec.Body.Bytes(), &resResp)
	if len(resResp.Reservations) != 1 || resResp.Reservations[0].WorkspaceID != "ws-mengwi" {
		t.Fatalf("user reservations = %s, want 1 booking for ws-mengwi", resRec.Body.String())
	}
}

func TestUserReservationsUnknownUser404s(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/users/usr_does_not_exist/reservations", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-del-1", "deleteme@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID string `json:"user_id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	start := time.Now().Add(120 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-sanur", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	delReq := httptest.NewRequest(http.MethodDelete, "/users/"+authResp.User.UserID, nil)
	delReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /users/{id} status = %d, want 200", delRec.Code)
	}

	mineReq := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	mineReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	mineRec := httptest.NewRecorder()
	h.ServeHTTP(mineRec, mineReq)
	if mineRec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /reservations/mine after user deletion: status = %d, want 401", mineRec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/reservations", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	var listResp struct {
		Reservations []struct {
			ReservationID string `json:"reservation_id"`
			Status        string `json:"status"`
		} `json:"reservations"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	foundCancelled := false
	for _, r := range listResp.Reservations {
		if r.ReservationID == created.ReservationID && r.Status == "cancelled" {
			foundCancelled = true
		}
	}
	if !foundCancelled {
		t.Fatalf("reservation %s not found cancelled after user deletion: %s", created.ReservationID, listRec.Body.String())
	}

	delReq2 := httptest.NewRequest(http.MethodDelete, "/users/"+authResp.User.UserID, nil)
	delReq2.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	delRec2 := httptest.NewRecorder()
	h.ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want 404", delRec2.Code)
	}
}
