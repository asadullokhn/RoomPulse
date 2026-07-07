package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateListCancelReservation(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-booking-1", "booker@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(authRec.Body.Bytes(), &authResp); err != nil || authResp.SessionToken == "" {
		t.Fatalf("sign-in failed: %s", authRec.Body.String())
	}

	// A room that exists in the mock Zoom seed (see zoom.NewMockClient's
	// default seed — ws-agung is one of the built-in Bali rooms).
	start := time.Now().Add(48 * time.Hour) // far in the future, clear of the mock seed's own reservations
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-agung", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s, want 200", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ReservationID string `json:"reservation_id"`
		Source        string `json:"source"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.Source != "app" {
		t.Fatalf("create response = %s", createRec.Body.String())
	}

	// Check in on the app-sourced reservation — the mock Zoom client has
	// never seen this ID, so if the Task 2 guard didn't skip the Zoom call
	// for Source == "app", this would 404 ("reservation not found in
	// zoom") instead of succeeding. This is what actually proves the guard.
	checkInReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/check-in", nil)
	checkInReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	checkInRec := httptest.NewRecorder()
	h.ServeHTTP(checkInRec, checkInReq)
	if checkInRec.Code != http.StatusOK {
		t.Fatalf("check-in on app-sourced reservation status = %d, body = %s, want 200 (Zoom call should have been skipped)", checkInRec.Code, checkInRec.Body.String())
	}

	// A second booking for the exact same room/window must conflict.
	conflictReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	conflictReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	conflictRec := httptest.NewRecorder()
	h.ServeHTTP(conflictRec, conflictReq)
	if conflictRec.Code != http.StatusConflict {
		t.Fatalf("conflicting create status = %d, want 409", conflictRec.Code)
	}

	// List mine — should contain exactly the one booking.
	listReq := httptest.NewRequest(http.MethodGet, "/reservations/mine", nil)
	listReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	var listResp struct {
		Reservations []struct {
			ReservationID string `json:"reservation_id"`
		} `json:"reservations"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil || len(listResp.Reservations) != 1 {
		t.Fatalf("list mine = %s, want exactly 1 reservation", listRec.Body.String())
	}

	// Cancel it, then a new booking for the same window should succeed.
	cancelReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s, want 200", cancelRec.Code, cancelRec.Body.String())
	}

	rebookReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	rebookReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	rebookRec := httptest.NewRecorder()
	h.ServeHTTP(rebookRec, rebookReq)
	if rebookRec.Code != http.StatusOK {
		t.Fatalf("rebook after cancel status = %d, want 200 (cancelled bookings shouldn't block)", rebookRec.Code)
	}
}

func TestCancelSomeoneElsesReservationForbidden(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	tokenA, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-A", "a@example.com")
	verifier.KeysURL = jwksURL

	authBodyA, _ := json.Marshal(map[string]string{"identity_token": tokenA})
	authReqA := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBodyA))
	authRecA := httptest.NewRecorder()
	h.ServeHTTP(authRecA, authReqA)
	var respA struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRecA.Body.Bytes(), &respA)

	start := time.Now().Add(72 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-bedugul", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+respA.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	// A different signed-in user tries to cancel A's booking.
	tokenB, jwksURLB := signAppleToken(t, "test.bundle.id", "apple-sub-B", "b@example.com")
	verifier.KeysURL = jwksURLB
	authBodyB, _ := json.Marshal(map[string]string{"identity_token": tokenB})
	authReqB := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBodyB))
	authRecB := httptest.NewRecorder()
	h.ServeHTTP(authRecB, authReqB)
	var respB struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRecB.Body.Bytes(), &respB)

	cancelReq := httptest.NewRequest(http.MethodPost, "/reservations/"+created.ReservationID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+respB.SessionToken)
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusForbidden {
		t.Fatalf("cancel-someone-else's status = %d, want 403", cancelRec.Code)
	}
}

func TestAdminCancelAnyReservation(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	token, jwksURL := signAppleToken(t, "test.bundle.id", "apple-sub-admin-cancel", "owner@example.com")
	verifier.KeysURL = jwksURL

	authBody, _ := json.Marshal(map[string]string{"identity_token": token})
	authReq := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(authBody))
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authReq)
	var authResp struct {
		SessionToken string `json:"session_token"`
	}
	_ = json.Unmarshal(authRec.Body.Bytes(), &authResp)

	start := time.Now().Add(96 * time.Hour)
	end := start.Add(time.Hour)
	createBody, _ := json.Marshal(map[string]any{
		"workspace_id": "ws-agung", "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+authResp.SessionToken)
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	var created struct {
		ReservationID string `json:"reservation_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.ReservationID == "" {
		t.Fatalf("create response = %s", createRec.Body.String())
	}

	// No Authorization header — admin cancel is unauthenticated by design.
	cancelReq := httptest.NewRequest(http.MethodPost, "/admin/reservations/"+created.ReservationID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("admin cancel status = %d, body = %s, want 200", cancelRec.Code, cancelRec.Body.String())
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(cancelRec.Body.Bytes(), &cancelled); err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("admin cancel response = %s, want status=cancelled", cancelRec.Body.String())
	}
}

func TestAdminCancelZoomSourcedForbidden(t *testing.T) {
	h := newTestHandler(t)

	// res-petang is seeded by the mock Zoom client — Zoom-sourced, not
	// cancellable through the admin app-booking endpoint.
	cancelReq := httptest.NewRequest(http.MethodPost, "/admin/reservations/res-petang/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+adminToken(t, h))
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusForbidden {
		t.Fatalf("admin cancel zoom-sourced status = %d, want 403", cancelRec.Code)
	}
}

func TestPatchReservationTitleAndWindow(t *testing.T) {
	h, verifier := newTestHandlerWithVerifier(t)
	owner := userSession(t, h, verifier, "apple-sub-patch-owner", "owner@example.com")

	start := time.Now().Add(72 * time.Hour)
	end := start.Add(time.Hour)
	rec := doAuth(t, h, http.MethodPost, "/reservations", owner, map[string]any{
		"workspace_id": "ws-agung", "title": "Design sync",
		"start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body)
	}
	var created struct {
		ReservationID string `json:"reservation_id"`
		Title         string `json:"title"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Title != "Design sync" {
		t.Fatalf("create response = %s, want title \"Design sync\"", rec.Body)
	}

	// Rename only.
	rec = doAuth(t, h, http.MethodPatch, "/reservations/"+created.ReservationID, owner, map[string]any{"title": "Retro"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch title status = %d body=%s", rec.Code, rec.Body)
	}
	var patched struct {
		Title   string    `json:"title"`
		EndTime time.Time `json:"end_time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || patched.Title != "Retro" {
		t.Fatalf("patch response = %s, want title \"Retro\"", rec.Body)
	}

	// Move the window; title must survive.
	newEnd := end.Add(30 * time.Minute)
	rec = doAuth(t, h, http.MethodPatch, "/reservations/"+created.ReservationID, owner, map[string]any{
		"end_time": newEnd.Format(time.RFC3339),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch window status = %d body=%s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || patched.Title != "Retro" || !patched.EndTime.After(end) {
		t.Fatalf("patch response = %s, want kept title and extended end", rec.Body)
	}

	// Empty patch is rejected.
	rec = doAuth(t, h, http.MethodPatch, "/reservations/"+created.ReservationID, owner, map[string]any{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty patch status = %d, want 422", rec.Code)
	}

	// Someone else can't edit it.
	stranger := userSession(t, h, verifier, "apple-sub-patch-stranger", "stranger@example.com")
	rec = doAuth(t, h, http.MethodPatch, "/reservations/"+created.ReservationID, stranger, map[string]any{"title": "Hijacked"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger patch status = %d, want 403", rec.Code)
	}

	// Cancelled bookings are immutable history.
	rec = doAuth(t, h, http.MethodPost, "/reservations/"+created.ReservationID+"/cancel", owner, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body=%s", rec.Code, rec.Body)
	}
	rec = doAuth(t, h, http.MethodPatch, "/reservations/"+created.ReservationID, owner, map[string]any{"title": "Zombie"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("patch cancelled status = %d, want 409", rec.Code)
	}
}
