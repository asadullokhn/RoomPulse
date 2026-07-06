# Sign-in Gatekeeper (#16) + APNs Notification Contract (#18) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make QuickRoom sign-in mandatory and expiry-aware (Rei's repo, issue #16), and give every APNs push the category/thread/collapse/interruption fields of the notification contract (this repo's backend, issue #18).

**Architecture:** Backend: new optional presentation fields on `apns.Notification`, written by `Push()`; one pure mapping function in `notify.go` applied at the single fan-out point. iOS: remove the onboarding skip, gate `ContentView` on the keychain session, and add an `onUnauthorized` hook to `APIClient` that AuthService wires to a local-only session clear.

**Tech Stack:** Go 1.x stdlib + golang-jwt (backend, `backend/`), Swift/SwiftUI + XCTest (iOS, `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom`).

**Spec:** `docs/superpowers/specs/2026-07-06-signin-gatekeeper-and-notification-contract-design.md`

## Global Constraints

- Two repos: Tasks 1–3 run in `/Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon` (commit to `main` directly), Tasks 4–6 in `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom` (feature branch `signin-gatekeeper`, PR to `main`; never push to Rei's `main`).
- Commit messages: concise, imperative, no emojis, **no Co-Authored-By lines**.
- No new dependencies in either repo.
- iOS files use tabs for indentation (match existing files exactly).
- iOS builds/tests need full Xcode: `export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer` before any `xcodebuild`.
- Contract values (copy verbatim, from the spec): `grace_reminder → GRACE_REMINDER / time-sensitive / grace-<reservation_id>`; `no_show_released → NO_SHOW_RELEASED / active / res-<reservation_id>`; `room_freed → ROOM_FREED / passive / freed-<workspace_id>`; `collision → COLLISION / time-sensitive / res-<reservation_id>`; `overstay → OVERSTAY / active / res-<reservation_id>`; `aps.thread-id = workspace_id` for all types.
- Do not modify VPS `docker-compose.yml` or `.env`; deploy is tar-pipe + rebuild (Task 3 has exact commands).

---

### Task 1: `apns.Notification` presentation fields

**Files:**
- Modify: `backend/internal/apns/apns.go` (struct at ~line 30, `Push` payload at ~line 116)
- Test: `backend/internal/apns/apns_test.go`

**Interfaces:**
- Produces: `apns.Notification` gains `Category, ThreadID, CollapseID, InterruptionLevel string` fields. `Push()` writes them as `aps.category`, `aps.thread-id`, `aps.interruption-level` (only when non-empty) and sends header `apns-collapse-id` when `CollapseID != ""`. Task 2 sets these fields.

- [ ] **Step 1: Write the failing test** — append to `backend/internal/apns/apns_test.go`:

```go
func TestPushSendsPresentationFields(t *testing.T) {
	var gotCollapse string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCollapse = r.Header.Get("apns-collapse-id")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	c, err := New(testKeyPEM(t), "K", "T", "topic", "h")
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	err = c.Push(context.Background(), "tok", Notification{
		Title: "t", Body: "b", Type: "grace_reminder",
		WorkspaceID: "ws-x", ReservationID: "res-1",
		Category: "GRACE_REMINDER", ThreadID: "ws-x",
		CollapseID: "grace-res-1", InterruptionLevel: "time-sensitive",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotCollapse != "grace-res-1" {
		t.Fatalf("apns-collapse-id = %q", gotCollapse)
	}
	aps := gotBody["aps"].(map[string]any)
	if aps["category"] != "GRACE_REMINDER" || aps["thread-id"] != "ws-x" || aps["interruption-level"] != "time-sensitive" {
		t.Fatalf("aps = %v", aps)
	}
}

func TestPushOmitsEmptyPresentationFields(t *testing.T) {
	var gotCollapse string
	var hasCollapseHeader bool
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCollapse = r.Header.Get("apns-collapse-id")
		_, hasCollapseHeader = r.Header[http.CanonicalHeaderKey("apns-collapse-id")]
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	_ = c.Push(context.Background(), "tok", Notification{Title: "t"})

	if hasCollapseHeader || gotCollapse != "" {
		t.Fatalf("expected no apns-collapse-id header, got %q", gotCollapse)
	}
	aps := gotBody["aps"].(map[string]any)
	for _, k := range []string{"category", "thread-id", "interruption-level"} {
		if _, ok := aps[k]; ok {
			t.Fatalf("aps should omit empty %s, aps = %v", k, aps)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./internal/apns/ -run TestPushSendsPresentationFields -v`
Expected: compile error — `unknown field Category in struct literal`

- [ ] **Step 3: Implement** — in `backend/internal/apns/apns.go`:

Extend the struct (keep the existing comment style):

```go
// Notification is one alert push. Type/WorkspaceID/ReservationID ride along
// as custom payload keys so the app can deep-link later. Category, ThreadID,
// CollapseID and InterruptionLevel are the notification-contract presentation
// fields (QuickRoom #18); empty values are omitted.
type Notification struct {
	Title             string
	Body              string
	Type              string
	WorkspaceID       string
	ReservationID     string
	Category          string
	ThreadID          string
	CollapseID        string
	InterruptionLevel string
}
```

In `Push()`, replace the payload construction with:

```go
	aps := map[string]any{
		"alert": map[string]any{"title": n.Title, "body": n.Body},
		"sound": "default",
	}
	if n.Category != "" {
		aps["category"] = n.Category
	}
	if n.ThreadID != "" {
		aps["thread-id"] = n.ThreadID
	}
	if n.InterruptionLevel != "" {
		aps["interruption-level"] = n.InterruptionLevel
	}
	payload := map[string]any{"aps": aps}
```

After the existing `req.Header.Set("apns-priority", "10")` line add:

```go
	if n.CollapseID != "" {
		req.Header.Set("apns-collapse-id", n.CollapseID)
	}
```

- [ ] **Step 4: Run the package tests**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./internal/apns/ -v`
Expected: all PASS (including the pre-existing `TestPushSendsWellFormedRequest`)

- [ ] **Step 5: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon
git add backend/internal/apns/apns.go backend/internal/apns/apns_test.go
git commit -m "APNs: carry category, thread-id, collapse-id, interruption-level"
```

---

### Task 2: Contract mapping at the fan-out point

**Files:**
- Modify: `backend/internal/api/notify.go` (`pushNotification`, ~line 167 payload literal)
- Test: `backend/internal/api/notify_test.go` (mapping table test), `backend/internal/api/apns_fanout_test.go` (extend `fakePusher` to record payloads)

**Interfaces:**
- Consumes: `apns.Notification` fields from Task 1.
- Produces: `func apnsFields(note Notification) (category, interruption, collapseID string)` in package `api`; `pushNotification` fills all four presentation fields (`ThreadID` = `note.WorkspaceID`).

- [ ] **Step 1: Write the failing mapping test** — append to `backend/internal/api/notify_test.go`:

```go
func TestAPNSFieldsPerType(t *testing.T) {
	cases := []struct {
		typ, resID, wsID                  string
		category, interruption, collapse string
	}{
		{"grace_reminder", "res-1", "ws-a", "GRACE_REMINDER", "time-sensitive", "grace-res-1"},
		{"no_show_released", "res-1", "ws-a", "NO_SHOW_RELEASED", "active", "res-res-1"},
		{"room_freed", "", "ws-a", "ROOM_FREED", "passive", "freed-ws-a"},
		{"collision", "res-2", "ws-b", "COLLISION", "time-sensitive", "res-res-2"},
		{"overstay", "res-3", "ws-b", "OVERSTAY", "active", "res-res-3"},
		{"unknown_type", "res-4", "ws-c", "", "", ""},
	}
	for _, c := range cases {
		cat, level, collapse := apnsFields(Notification{Type: c.typ, ReservationID: c.resID, WorkspaceID: c.wsID})
		if cat != c.category || level != c.interruption || collapse != c.collapse {
			t.Fatalf("%s: got (%q,%q,%q), want (%q,%q,%q)", c.typ, cat, level, collapse, c.category, c.interruption, c.collapse)
		}
	}
}

func TestAPNSFieldsEmptyIDMeansNoCollapse(t *testing.T) {
	if _, _, collapse := apnsFields(Notification{Type: "grace_reminder"}); collapse != "" {
		t.Fatalf("collapse for empty reservation id = %q, want empty", collapse)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./internal/api/ -run TestAPNSFields -v`
Expected: compile error — `undefined: apnsFields`

- [ ] **Step 3: Implement the mapping** — add to `backend/internal/api/notify.go` above `pushNotification`:

```go
// apnsFields maps an outbox notification type to the APNs presentation fields
// of the notification contract (QuickRoom #18). Unknown types get no extras;
// a missing id suppresses the collapse key rather than emitting "grace-".
func apnsFields(note Notification) (category, interruption, collapseID string) {
	collapse := func(prefix, id string) string {
		if id == "" {
			return ""
		}
		return prefix + id
	}
	switch note.Type {
	case "grace_reminder":
		return "GRACE_REMINDER", "time-sensitive", collapse("grace-", note.ReservationID)
	case "no_show_released":
		return "NO_SHOW_RELEASED", "active", collapse("res-", note.ReservationID)
	case "room_freed":
		return "ROOM_FREED", "passive", collapse("freed-", note.WorkspaceID)
	case "collision":
		return "COLLISION", "time-sensitive", collapse("res-", note.ReservationID)
	case "overstay":
		return "OVERSTAY", "active", collapse("res-", note.ReservationID)
	}
	return "", "", ""
}
```

In `pushNotification`, replace the payload literal:

```go
	category, interruption, collapseID := apnsFields(note)
	payload := apns.Notification{
		Title: note.Title, Body: note.Body, Type: note.Type,
		WorkspaceID: note.WorkspaceID, ReservationID: note.ReservationID,
		Category: category, ThreadID: note.WorkspaceID,
		CollapseID: collapseID, InterruptionLevel: interruption,
	}
```

- [ ] **Step 4: Run mapping tests**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./internal/api/ -run TestAPNSFields -v`
Expected: PASS

- [ ] **Step 5: Write the failing fan-out payload test** — in `backend/internal/api/apns_fanout_test.go`, extend `fakePusher` to record payloads (replace the struct and `Push`; `tokens()` stays):

```go
type fakePusher struct {
	mu       sync.Mutex
	calls    []string // device tokens pushed to
	payloads []apns.Notification
	fail     map[string]error
}

func (f *fakePusher) Push(_ context.Context, tok string, n apns.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tok)
	f.payloads = append(f.payloads, n)
	if f.fail != nil {
		return f.fail[tok]
	}
	return nil
}

func (f *fakePusher) lastPayload() apns.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.payloads) == 0 {
		return apns.Notification{}
	}
	return f.payloads[len(f.payloads)-1]
}
```

Append the test:

```go
func TestEmitAppliesNotificationContract(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-1", "booker@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	s.notify.emit("kc", Notification{
		Type: "grace_reminder", Recipient: "booker@x.y",
		WorkspaceID: "ws-7", ReservationID: "res-9", Title: "t", Body: "b",
	})
	waitFor(t, func() bool { return len(fp.tokens()) == 1 })

	got := fp.lastPayload()
	if got.Category != "GRACE_REMINDER" || got.InterruptionLevel != "time-sensitive" ||
		got.CollapseID != "grace-res-9" || got.ThreadID != "ws-7" {
		t.Fatalf("payload = %+v", got)
	}
}
```

- [ ] **Step 6: Run the full api + apns packages**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./internal/api/ ./internal/apns/`
Expected: all PASS (fan-out contract test passes because Step 3 already wired the mapping; if it fails, the wiring in `pushNotification` is wrong)

- [ ] **Step 7: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon
git add backend/internal/api/notify.go backend/internal/api/notify_test.go backend/internal/api/apns_fanout_test.go
git commit -m "Notifications: apply per-type APNs contract at fan-out (#18 backend)"
```

---

### Task 3: Deploy backend + post the contract on #18

**Files:**
- No source changes. Runs the standard deploy, then comments on the issue.

**Interfaces:**
- Consumes: merged Task 1+2 code on local `main`.
- Produces: live backend on rp.asadullokhn.uz; contract comment on `Reishandy/QuickRoom#18`.

- [ ] **Step 1: Build the frontend (required before every deploy — `go:embed web/dist`)**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/frontend && npm run build
```
Expected: Vite build succeeds, output written to `backend/internal/api/web/dist`.

- [ ] **Step 2: Run the full backend test suite**

Run: `cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend && go test ./...`
Expected: all packages PASS

- [ ] **Step 3: Ship the working tree and rebuild** (exact tar excludes matter — never exclude `roompulse` or `out`):

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon/backend
COPYFILE_DISABLE=1 tar czf - --exclude='.env' --exclude='docker-compose.yml' \
  --exclude='.git' --exclude='*.db*' --exclude='.DS_Store' --exclude='._*' . \
| ssh pr-diriger-hetzner 'tar xzf - -C /root/roompulse'
ssh pr-diriger-hetzner 'cd /root/roompulse && docker compose -p backend up -d --build'
```
Expected: image rebuilds, container restarts.

- [ ] **Step 4: Verify live**

```bash
ssh pr-diriger-hetzner 'ls /root/roompulse/cmd/*/main.go && docker logs roompulse --since 2m 2>&1 | tail -20'
curl -s https://rp.asadullokhn.uz/rooms | head -c 200
```
Expected: `cmd/quickroom/main.go` present, logs clean (no `apns push disabled`), rooms JSON returned.

- [ ] **Step 5: Post the contract comment**

```bash
gh issue comment 18 -R Reishandy/QuickRoom --body "$(cat <<'EOF'
Backend half is live. Every push now carries the contract fields — here is what the iOS side can rely on:

| type | aps.category | aps.interruption-level | apns-collapse-id | aps.thread-id |
|---|---|---|---|---|
| grace_reminder | GRACE_REMINDER | time-sensitive | grace-<reservation_id> | workspace_id |
| no_show_released | NO_SHOW_RELEASED | active | res-<reservation_id> | workspace_id |
| room_freed | ROOM_FREED | passive | freed-<workspace_id> | workspace_id |
| collision | COLLISION | time-sensitive | res-<reservation_id> | workspace_id |
| overstay | OVERSTAY | active | res-<reservation_id> | workspace_id |

- Custom keys unchanged: `type`, `workspace_id`, `reservation_id` (top-level) for deep-linking.
- Grace ladder levels share one collapse id, so level 2 replaces level 1 instead of stacking.
- `room_freed` is a broadcast and ships passive so it lands quietly in the list.
- Suggested actions for the GRACE_REMINDER category on your side: "I'm here" -> `POST /presence`, "Release" -> `POST /reservations/{id}/cancel`. The app already has the time-sensitive entitlement.
- Copy/frequency tuning intentionally untouched pending your team discussion.
EOF
)"
```

- [ ] **Step 6: Push the repo**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon && git push
```

---

### Task 4: iOS — remove skip, gate on the session

**Files:**
- Modify: `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom/QuickRoom/UI/Onboarding/OnboardingView.swift`
- Modify: `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom/QuickRoom/UI/Main/ContentView.swift:32-39,58`

**Interfaces:**
- Consumes: existing `AuthService.isSignedIn`, `PreferenceService.hasSeenOnboarding` (both already in the environment).
- Produces: main app unreachable while signed out; Task 5's expiry handling relies on this gate.

- [ ] **Step 1: Create the branch (from up-to-date main)**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom
git fetch origin && git checkout main && git pull --ff-only && git checkout -b signin-gatekeeper
```

- [ ] **Step 2: Edit `OnboardingView.swift`** — replace the skip/continue button block (lines 44-47) so Continue appears only when already signed in:

```swift
			if authService.isSignedIn {
				Button("Continue") {
					preferenceService.hasSeenOnboarding = true
				}
				.buttonStyle(.borderedProminent)
			}
```

(The `SignInWithAppleButton` block stays as is — successful sign-in already sets `hasSeenOnboarding = true`.)

- [ ] **Step 3: Edit `ContentView.swift`** — add the auth service to the gate. Add the environment property after line 14:

```swift
	@Environment(AuthService.self) private var authService
```

Change the gate (lines 33-39) to:

```swift
			if preferenceService.hasSeenOnboarding && authService.isSignedIn {
				baseScreen
			} else {
				OnboardingView()
			}
```

And extend the animation line 58:

```swift
		.animation(.easeInOut, value: preferenceService.hasSeenOnboarding)
		.animation(.easeInOut, value: authService.isSignedIn)
```

- [ ] **Step 4: Build**

```bash
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom
xcodebuild -project QuickRoom.xcodeproj -scheme QuickRoom -configuration Debug \
  -destination 'platform=iOS Simulator,name=iPhone 17' build 2>&1 | tail -3
```
Expected: `** BUILD SUCCEEDED **`

- [ ] **Step 5: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom
git add QuickRoom/UI/Onboarding/OnboardingView.swift QuickRoom/UI/Main/ContentView.swift
git commit -m "Require Sign in with Apple: no skip, gate main app on session"
```

---

### Task 5: iOS — 401 clears the session locally

**Files:**
- Modify: `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom/QuickRoom/Core/Network/APIClient.swift:26-37,74`
- Modify: `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom/QuickRoom/Core/Services/AuthService.swift:22-27,60-65`
- Test: `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom/QuickRoomTests/AuthServiceTests.swift`

**Interfaces:**
- Consumes: `APIClient` singleton pattern, `KeychainStore.sessionToken` / `currentUserJSON`.
- Produces: `APIClient.onUnauthorized: (() -> Void)?` (fired on every 401); `@MainActor func handleUnauthorized()` on `AuthService` (local clear, no network).

- [ ] **Step 1: Write the failing test** — append to `AuthServiceTests.swift`:

```swift
	@MainActor
	func testHandleUnauthorizedClearsSessionLocally() {
		KeychainStore.sessionToken = "tok-dead"
		KeychainStore.currentUserJSON = #"{"userId":"u-1","email":"a@b.c","name":"Asadullokh"}"#
		let service = AuthService(client: .shared)
		XCTAssertTrue(service.isSignedIn)

		service.handleUnauthorized()

		XCTAssertFalse(service.isSignedIn)
		XCTAssertNil(KeychainStore.sessionToken)
		XCTAssertNil(KeychainStore.currentUserJSON)
	}

	@MainActor
	func testInitWiresUnauthorizedHook() {
		KeychainStore.sessionToken = "tok-dead"
		KeychainStore.currentUserJSON = #"{"userId":"u-1","email":"a@b.c","name":"Asadullokh"}"#
		let client = APIClient(baseURL: URL(string: "http://localhost:1")!, tokenProvider: { KeychainStore.sessionToken })
		let service = AuthService(client: client)
		XCTAssertTrue(service.isSignedIn)

		client.onUnauthorized?()

		let cleared = expectation(description: "session cleared")
		Task { @MainActor in
			while service.isSignedIn { await Task.yield() }
			cleared.fulfill()
		}
		wait(for: [cleared], timeout: 2)
	}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom
xcodebuild test -project QuickRoom.xcodeproj -scheme QuickRoom \
  -destination 'platform=iOS Simulator,name=iPhone 17' \
  -only-testing:QuickRoomTests/AuthServiceTests 2>&1 | tail -5
```
Expected: compile failure — `value of type 'AuthService' has no member 'handleUnauthorized'`

- [ ] **Step 3: Implement** — in `APIClient.swift`, add the hook property after `private let tokenProvider` (line 31):

```swift
	/// Fired on any 401 so AuthService can drop the dead session. Set once at
	/// AuthService init; called off the main actor.
	var onUnauthorized: (() -> Void)?
```

In `send`, change the 401 arm (line 74):

```swift
			case 401:
				onUnauthorized?()
				throw APIError.unauthorized
```

In `AuthService.swift`, wire it at the end of `init`:

```swift
	init(client: APIClient = .shared) {
		self.client = client
		if KeychainStore.sessionToken != nil, let json = KeychainStore.currentUserJSON {
			currentUser = try? JSONDecoder().decode(UserDTO.self, from: Data(json.utf8))
		}
		client.onUnauthorized = { [weak self] in
			Task { @MainActor in self?.handleUnauthorized() }
		}
	}
```

Add the handler after `signOut()`:

```swift
	/// The backend rejected our token (expired or revoked). Clear the session
	/// locally — no /auth/logout call: the token is already dead, and a network
	/// call from here could loop straight back into another 401.
	@MainActor
	func handleUnauthorized() {
		KeychainStore.sessionToken = nil
		KeychainStore.currentUserJSON = nil
		currentUser = nil
	}
```

- [ ] **Step 4: Run the test class**

Same command as Step 2. Expected: `** TEST SUCCEEDED **` (all AuthServiceTests pass, including the pre-existing three)

- [ ] **Step 5: Run the whole test bundle** (guard against regressions)

```bash
xcodebuild test -project QuickRoom.xcodeproj -scheme QuickRoom \
  -destination 'platform=iOS Simulator,name=iPhone 17' 2>&1 | tail -5
```
Expected: `** TEST SUCCEEDED **`

- [ ] **Step 6: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom
git add QuickRoom/Core/Network/APIClient.swift QuickRoom/Core/Services/AuthService.swift QuickRoomTests/AuthServiceTests.swift
git commit -m "Clear session locally on 401 so expiry returns to sign-in"
```

---

### Task 6: PR to Rei's repo

**Files:**
- No source changes.

**Interfaces:**
- Consumes: branch `signin-gatekeeper` with Tasks 4–5 committed.
- Produces: open PR on `Reishandy/QuickRoom` closing #16.

- [ ] **Step 1: Push the branch**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git push -u origin signin-gatekeeper
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create -R Reishandy/QuickRoom --base main --head signin-gatekeeper \
  --title "Sign-in gatekeeper: no skip, session-gated app, 401 signs out" \
  --body "$(cat <<'EOF'
Closes #16.

- Onboarding: "Skip for now" removed; Continue only appears when a session already exists (keychain survives reinstall). Successful Sign in with Apple advances as before.
- ContentView gates the main app on `hasSeenOnboarding && authService.isSignedIn`, so sign-out or an expired session returns to onboarding.
- APIClient fires a new `onUnauthorized` hook on any 401; AuthService clears the session locally (keychain + currentUser, deliberately no `/auth/logout` — the token is already dead). Covered by new unit tests in AuthServiceTests.

Note for verification: my free-team build can't sign the SIWA entitlement, so I've verified the gate + 401 path via unit tests and simulator; please run the sign-in happy path on your signed build before merging.
EOF
)"
```

- [ ] **Step 3: Log the work** — append a row to the "2026-07-06" section of `/Users/asadullokhn/ObsidianVault/Default/Projects/Personal/RoomPulse/Challenge Work Log.md` recording both deliverables (backend contract deployed; gatekeeper PR opened with its URL).
