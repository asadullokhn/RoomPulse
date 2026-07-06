# Sign-in gatekeeper (QuickRoom #16) + APNs notification contract (QuickRoom #18) — design

Date: 2026-07-06
Issues: [Reishandy/QuickRoom#16](https://github.com/Reishandy/QuickRoom/issues/16),
[Reishandy/QuickRoom#18](https://github.com/Reishandy/QuickRoom/issues/18)
Repos touched: `Reishandy/QuickRoom` (iOS, #16) and this repo's `backend/` (#18).

## Context

- APNs delivery already works end to end (#8): `internal/apns` client (p8/ES256, JWT
  cached ~50 min), token registry in SQLite, fan-out in `notify.go` `pushNotification`,
  dead tokens pruned on `ErrUnregistered`. Rei confirmed sandbox pushes on device.
- The payload today: `aps.alert` + `sound: default`, custom keys `type`,
  `workspace_id`, `reservation_id`. No category, no thread-id, no collapse, no
  interruption level.
- iOS sign-in today: `OnboardingView` placeholder with a SIWA button **and a
  "Skip for now" button** that sets `hasSeenOnboarding` without auth. `ContentView`
  gates only on `hasSeenOnboarding`. `APIClient` throws `APIError.unauthorized` on
  401 and nothing reacts.
- Constraint: Asadullokh's free team cannot sign the SIWA entitlement, so on-device
  E2E of sign-in happens on Rei's builds. After #16 lands, local free-team builds
  stop at the sign-in step (accepted; team-signing access is being sorted separately).

## Part 1 — #16 sign-in gatekeeper (Rei's repo)

Scope decision (user): full gatekeeper, including 401 handling.

1. **No skip.** Remove "Skip for now" from `OnboardingView`. Successful
   `completeSignIn` still sets `hasSeenOnboarding = true`. Keep a Continue button
   only for the `isSignedIn && !hasSeenOnboarding` case (keychain survives app
   reinstall, so this state is reachable).
2. **Gate on the session.** `ContentView` shows `baseScreen` only when
   `preferenceService.hasSeenOnboarding && authService.isSignedIn`; otherwise
   `OnboardingView`. Add `authService.isSignedIn` to the existing `.animation`
   modifier. `AuthService` is already injected via environment.
3. **401 → local sign-out.** `APIClient` gains `var onUnauthorized: (() -> Void)?`,
   fired at the existing `case 401` mapping. `AuthService` sets it in `init` to a
   local-only clear: keychain token + `currentUserJSON` + `currentUser = nil`. It
   must NOT call `/auth/logout` (token already dead; avoids request loops). The UI
   returns to onboarding through the same gate.

Non-goals: onboarding visual design (#12), any change to `AuthService.completeSignIn`
or the backend auth endpoints.

Verification: `QuickRoomTests` has real unit tests (APIClient, AuthService,
Presence, ReservationService) — add a test that the `onUnauthorized` hook clears
the keychain session and `currentUser` without a network call. Build + run tests
for device/simulator; SIWA happy path verified by Rei on his signed build
(stated in the PR).

## Part 2 — #18 notification contract + backend support (this repo)

Scope decision (user): contract + backend implementation; frequency/copy tuning
stays open for Rei's team discussion.

### Contract (per outbox `type`)

| type | category | interruption-level | apns-collapse-id | iOS actions (Rei's half) |
|---|---|---|---|---|
| `grace_reminder` | `GRACE_REMINDER` | `time-sensitive` | `grace-<reservation_id>` | "I'm here" → `POST /presence`; "Release" → `POST /reservations/{id}/cancel` |
| `no_show_released` | `NO_SHOW_RELEASED` | `active` | `res-<reservation_id>` | none |
| `room_freed` | `ROOM_FREED` | `passive` | `freed-<workspace_id>` | none |
| `collision` | `COLLISION` | `time-sensitive` | `res-<reservation_id>` | none |
| `overstay` | `OVERSTAY` | `active` | `res-<reservation_id>` | none |

- `aps.thread-id` = `workspace_id` for every type (notifications group by room).
- Grace ladder reminders share one collapse id, so level 2 replaces level 1
  instead of stacking.
- `room_freed` is a broadcast; `passive` keeps it out of people's faces.
- Deep-link custom keys `type` / `workspace_id` / `reservation_id` are unchanged.
- The app already holds the `time-sensitive` entitlement.

### Implementation

- `apns.Notification` gains optional `Category`, `ThreadID`, `CollapseID`,
  `InterruptionLevel`. `Push()` writes `aps.category`, `aps.thread-id`,
  `aps.interruption-level` when set, and sends the `apns-collapse-id` header
  when set.
- One mapping function in `notify.go` (`apnsFieldsFor(note Notification)`)
  applied inside `pushNotification`. Emit sites stay untouched.
- Tests: extend `internal/apns` payload assertions and the fan-out test with the
  new fields; table-test the mapping function.
- After merge: deploy via the usual tar + rebuild, then post the contract as a
  comment on #18 for the iOS half.

Non-goals: per-user notification preferences / quiet hours (premature before the
team discussion), changes to outbox semantics or the `/notifications` endpoint.
